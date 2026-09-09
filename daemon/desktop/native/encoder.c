#define _POSIX_C_SOURCE 200809L
#include "encoder.h"
#include <gst/app/gstappsink.h>
#include <gst/app/gstappsrc.h>
#include <gst/video/video.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <unistd.h>
#include <string.h>

static gboolean fail(GError **error, const char *reason) {
  g_set_error_literal(error, G_IO_ERROR, G_IO_ERROR_NOT_SUPPORTED, reason);
  return FALSE;
}

gboolean zen_encoder_config_valid(const ZenEncoderConfig *c) {
  return c && c->width >= 2 && c->width <= 1280 && !(c->width & 1) &&
    c->height >= 2 && c->height <= 720 && !(c->height & 1) &&
    c->fps >= 1 && c->fps <= 30 && c->bitrate >= 1000 && c->bitrate <= 4000000;
}

gboolean zen_encoder_render_node_valid(const char *path) {
  if (!path || !g_str_has_prefix(path, "/dev/dri/renderD")) return FALSE;
  const char *number = path + strlen("/dev/dri/renderD");
  if (!*number) return FALSE;
  for (const char *p = number; *p; p++) if (!g_ascii_isdigit(*p)) return FALSE;
  guint64 node = g_ascii_strtoull(number, NULL, 10);
  struct stat st;
  return node >= 128 && node <= 255 && lstat(path, &st) == 0 && S_ISCHR(st.st_mode) &&
    major(st.st_rdev) == 226 && minor(st.st_rdev) == node && access(path, R_OK | W_OK) == 0;
}

static GstCaps *raw_caps(const ZenEncoderConfig *c, gboolean hardware) {
  return gst_caps_new_simple("video/x-raw", "format", G_TYPE_STRING, hardware ? "NV12" : "I420",
    "width", G_TYPE_INT, c->width, "height", G_TYPE_INT, c->height,
    "framerate", GST_TYPE_FRACTION, c->fps, 1, NULL);
}

static GstCaps *encoded_caps(const ZenEncoderConfig *c) {
  return gst_caps_new_simple("video/x-h264", "stream-format", G_TYPE_STRING, "byte-stream",
    "alignment", G_TYPE_STRING, "au", "profile", G_TYPE_STRING, "constrained-baseline",
    "width", G_TYPE_INT, c->width, "height", G_TYPE_INT, c->height, NULL);
}

gboolean zen_encoder_caps_supported(const ZenEncoderConfig *c, GstCaps *sink, GstCaps *src, gboolean hardware) {
  if (!zen_encoder_config_valid(c) || !sink || !src || gst_caps_is_any(sink) || gst_caps_is_any(src) ||
      gst_caps_is_empty(sink) || gst_caps_is_empty(src)) return FALSE;
  GstCaps *raw = raw_caps(c, hardware), *encoded = encoded_caps(c);
  gboolean ok = gst_caps_can_intersect(sink, raw) && gst_caps_can_intersect(src, encoded);
  gst_caps_unref(raw); gst_caps_unref(encoded);
  return ok;
}

static gboolean uint_property(GstElement *element, const char *name, guint value) {
  GParamSpec *spec = g_object_class_find_property(G_OBJECT_GET_CLASS(element), name);
  if (!spec || !(spec->flags & G_PARAM_WRITABLE) || !G_IS_PARAM_SPEC_UINT(spec)) return FALSE;
  GParamSpecUInt *range = G_PARAM_SPEC_UINT(spec);
  if (value < range->minimum || value > range->maximum) return FALSE;
  g_object_set(element, name, value, NULL);
  return TRUE;
}

static gboolean enum_property(GstElement *element, const char *name, const char *nick) {
  GParamSpec *spec = g_object_class_find_property(G_OBJECT_GET_CLASS(element), name);
  if (!spec || !(spec->flags & G_PARAM_WRITABLE) || !G_IS_PARAM_SPEC_ENUM(spec)) return FALSE;
  GEnumClass *values = g_type_class_ref(G_PARAM_SPEC_VALUE_TYPE(spec));
  GEnumValue *value = g_enum_get_value_by_nick(values, nick);
  gboolean ok = value != NULL;
  if (ok) g_object_set(element, name, value->value, NULL);
  g_type_class_unref(values);
  return ok;
}

static gboolean configure(GstElement *encoder, const ZenEncoderConfig *c, gboolean hardware, GError **error) {
  if (hardware) {
    GParamSpec *device = g_object_class_find_property(G_OBJECT_GET_CLASS(encoder), "device-path");
    if (!device || !(device->flags & G_PARAM_READABLE) || G_PARAM_SPEC_VALUE_TYPE(device) != G_TYPE_STRING)
      return fail(error, "hardware_device_property_missing");
    gchar *actual = NULL;
    g_object_get(encoder, "device-path", &actual, NULL);
    gboolean matches = actual && c->render_node && !strcmp(actual, c->render_node);
    g_free(actual);
    if (!matches) return fail(error, "hardware_device_mismatch");
    if (!uint_property(encoder, "bitrate", c->bitrate / 1000) ||
        !uint_property(encoder, "key-int-max", c->fps) || !uint_property(encoder, "b-frames", 0) ||
        !enum_property(encoder, "rate-control", "cbr")) return fail(error, "hardware_low_latency_policy_unavailable");
  } else if (!uint_property(encoder, "bitrate", c->bitrate) || !uint_property(encoder, "max-bitrate", c->bitrate) ||
      !uint_property(encoder, "gop-size", c->fps) || !uint_property(encoder, "multi-thread", 2) ||
      !enum_property(encoder, "usage-type", "screen") || !enum_property(encoder, "complexity", "low") ||
      !enum_property(encoder, "rate-control", "bitrate")) return fail(error, "software_encoder_policy_unavailable");
  return TRUE;
}

static GstElement *chain(const ZenEncoderConfig *c, const char *factory, gboolean hardware, GError **error) {
  GstElement *bin = gst_bin_new(NULL);
  const char *names[] = {"videoconvert", "videoscale", "capsfilter", factory, "h264parse", "capsfilter"};
  GstElement *elements[G_N_ELEMENTS(names)] = {0};
  for (guint i = 0; i < G_N_ELEMENTS(names); i++) {
    elements[i] = gst_element_factory_make(names[i], i == 3 ? "codec" : NULL);
    if (!elements[i]) { fail(error, "encoder_plugin_unavailable"); goto failed; }
    gst_bin_add(GST_BIN(bin), elements[i]);
  }
  if (!configure(elements[3], c, hardware, error)) goto failed;
  GstStateChangeReturn ready = gst_element_set_state(elements[3], GST_STATE_READY);
  if (ready != GST_STATE_CHANGE_FAILURE) ready = gst_element_get_state(elements[3], NULL, NULL, 500 * GST_MSECOND);
  if (ready == GST_STATE_CHANGE_FAILURE || ready == GST_STATE_CHANGE_ASYNC) {
    fail(error, "encoder_device_not_ready"); goto failed;
  }
  GstPad *sink = gst_element_get_static_pad(elements[3], "sink"), *src = gst_element_get_static_pad(elements[3], "src");
  GstCaps *sink_caps = sink ? gst_pad_query_caps(sink, NULL) : NULL;
  GstCaps *src_caps = src ? gst_pad_query_caps(src, NULL) : NULL;
  gboolean supported = zen_encoder_caps_supported(c, sink_caps, src_caps, hardware);
  if (sink_caps) gst_caps_unref(sink_caps);
  if (src_caps) gst_caps_unref(src_caps);
  if (sink) gst_object_unref(sink);
  if (src) gst_object_unref(src);
  if (!supported) { fail(error, "encoder_caps_incompatible"); goto failed; }
  gst_element_set_state(elements[3], GST_STATE_NULL);
  GstCaps *raw = raw_caps(c, hardware), *encoded = encoded_caps(c);
  g_object_set(elements[2], "caps", raw, NULL);
  g_object_set(elements[4], "config-interval", -1, NULL);
  g_object_set(elements[5], "caps", encoded, NULL);
  gst_caps_unref(raw); gst_caps_unref(encoded);
  for (guint i = 1; i < G_N_ELEMENTS(elements); i++) {
    if (!gst_element_link(elements[i - 1], elements[i])) { fail(error, "encoder_link_failed"); goto failed; }
  }
  sink = gst_element_get_static_pad(elements[0], "sink");
  src = gst_element_get_static_pad(elements[5], "src");
  gst_element_add_pad(bin, gst_ghost_pad_new("sink", sink));
  gst_element_add_pad(bin, gst_ghost_pad_new("src", src));
  gst_object_unref(sink); gst_object_unref(src);
  return bin;
failed:
  gst_element_set_state(bin, GST_STATE_NULL);
  /* A child may have entered READY before its bin did. */
  if (elements[3]) gst_element_set_state(elements[3], GST_STATE_NULL);
  gst_object_unref(bin);
  return NULL;
}

gboolean zen_encoder_probe(GstElement *bin, const ZenEncoderConfig *c, GError **error) {
  GstElement *test = gst_pipeline_new(NULL), *source = gst_element_factory_make("appsrc", NULL);
  GstElement *sink = gst_element_factory_make("appsink", NULL);
  gboolean ok = FALSE;
  if (!source || !sink || !bin || !zen_encoder_config_valid(c)) {
    if (source) gst_object_unref(source);
    if (sink) gst_object_unref(sink);
    if (bin) gst_object_unref(bin);
    gst_object_unref(test);
    return fail(error, "encoder_probe_plugins_unavailable");
  }
  gst_bin_add_many(GST_BIN(test), source, bin, sink, NULL);
  GstVideoInfo info;
  gst_video_info_set_format(&info, GST_VIDEO_FORMAT_I420, c->width, c->height);
  GstCaps *raw = raw_caps(c, FALSE);
  gst_app_src_set_caps(GST_APP_SRC(source), raw); gst_caps_unref(raw);
  gst_app_src_set_max_bytes(GST_APP_SRC(source), info.size * 3);
  g_object_set(source, "format", GST_FORMAT_TIME, NULL);
  g_object_set(sink, "sync", FALSE, "max-buffers", 3, "drop", FALSE, NULL);
  if (!gst_element_link_many(source, bin, sink, NULL)) { fail(error, "encoder_probe_link_failed"); goto done; }
  if (c->cancel && g_cancellable_set_error_if_cancelled(c->cancel, error)) goto done;
  if (gst_element_set_state(test, GST_STATE_PLAYING) == GST_STATE_CHANGE_FAILURE) {
    fail(error, "encoder_probe_start_failed"); goto done;
  }
  for (guint i = 0; i < 3; i++) {
    GstBuffer *buffer = gst_buffer_new_allocate(NULL, info.size, NULL);
    if (!buffer) { fail(error, "encoder_probe_allocation_failed"); goto done; }
    gst_buffer_memset(buffer, 0, 0, info.size);
    GST_BUFFER_PTS(buffer) = i * GST_SECOND / c->fps;
    GST_BUFFER_DURATION(buffer) = GST_SECOND / c->fps;
    if (gst_app_src_push_buffer(GST_APP_SRC(source), buffer) != GST_FLOW_OK) {
      fail(error, "encoder_probe_input_failed"); goto done;
    }
  }
  gst_app_src_end_of_stream(GST_APP_SRC(source));
  gint64 deadline = g_get_monotonic_time() + 2 * G_TIME_SPAN_SECOND;
  guint samples = 0;
  GstCaps *expected = encoded_caps(c);
  while (samples < 3 && g_get_monotonic_time() < deadline) {
    for (guint i = 0; i < 8 && g_main_context_iteration(NULL, FALSE); i++) {}
    if (c->cancel && g_cancellable_set_error_if_cancelled(c->cancel, error)) break;
    GstSample *sample = gst_app_sink_try_pull_sample(GST_APP_SINK(sink), 50 * GST_MSECOND);
    if (!sample) { if (gst_app_sink_is_eos(GST_APP_SINK(sink))) break; continue; }
    GstCaps *caps = gst_sample_get_caps(sample);
    GstBuffer *buffer = gst_sample_get_buffer(sample);
    gboolean valid = caps && gst_caps_is_fixed(caps) && gst_caps_is_subset(caps, expected) &&
      buffer && gst_buffer_get_size(buffer) > 0 && gst_buffer_get_size(buffer) < (4u << 20);
    gst_sample_unref(sample);
    if (!valid) { fail(error, "encoder_probe_output_incompatible"); break; }
    samples++;
  }
  gst_caps_unref(expected);
  ok = samples == 3;
  if (!ok && (!error || !*error)) fail(error, "encoder_probe_incomplete");
done:
  gst_element_set_state(test, GST_STATE_NULL);
  gst_object_unref(test);
  return ok;
}

GstElement *zen_encoder_try_factory(const ZenEncoderConfig *c, const char *factory, gboolean hardware, gpointer data, GError **error) {
  (void)data;
  if (!zen_encoder_config_valid(c)) { fail(error, "invalid_encoder_configuration"); return NULL; }
  if (c->cancel && g_cancellable_set_error_if_cancelled(c->cancel, error)) return NULL;
  GstElement *bin = chain(c, factory, hardware, error);
  if (!bin || !zen_encoder_probe(bin, c, error)) return NULL;
  return chain(c, factory, hardware, error);
}

gboolean zen_encoder_select_verified(const ZenEncoderConfig *c, gboolean device_valid,
    const char *const *factories, guint count, ZenEncoderAttempt try_encoder, gpointer data,
    ZenEncoderSelection *selection, GError **error) {
  *selection = (ZenEncoderSelection){0};
  if (!zen_encoder_config_valid(c)) return fail(error, "invalid_encoder_configuration");
  if (c->cancel && g_cancellable_set_error_if_cancelled(c->cancel, error)) return FALSE;
  selection->reason = !c->render_node || !*c->render_node ? "software_default" :
    !device_valid ? "invalid_render_node" : "no_compatible_hardware_factory";
  if (c->render_node && *c->render_node && device_valid) for (guint i = 0; i < MIN(count, 8); i++) {
    GError *failed = NULL;
    selection->bin = try_encoder(c, factories[i], TRUE, data, &failed);
    if (selection->bin) {
      selection->hardware = TRUE; selection->name = "va-h264-hardware"; selection->reason = "hardware_probe_passed";
      g_clear_error(&failed); return TRUE;
    }
    g_printerr("zen-desktop: encoder=%s rejected: %s\n", factories[i], failed ? failed->message : "probe_failed");
    g_clear_error(&failed);
    if (c->cancel && g_cancellable_set_error_if_cancelled(c->cancel, error)) return FALSE;
    selection->reason = "hardware_probe_failed";
  }
  selection->name = "openh264-software";
  selection->bin = try_encoder(c, "openh264enc", FALSE, data, error);
  return selection->bin != NULL;
}

static gint factory_order(gconstpointer a, gconstpointer b) {
  return g_strcmp0(GST_OBJECT_NAME(a), GST_OBJECT_NAME(b));
}

gboolean zen_encoder_select(const ZenEncoderConfig *c, ZenEncoderSelection *selection, GError **error) {
  gboolean valid = zen_encoder_render_node_valid(c ? c->render_node : NULL);
  GPtrArray *names = g_ptr_array_new_with_free_func(g_free);
  if (valid) {
    GstCaps *h264 = gst_caps_new_empty_simple("video/x-h264");
    GList *factories = gst_element_factory_list_get_elements(GST_ELEMENT_FACTORY_TYPE_VIDEO_ENCODER, GST_RANK_NONE);
    factories = g_list_sort(factories, factory_order);
    for (GList *item = factories; item; item = item->next) {
      GstElementFactory *factory = item->data;
      if (!g_strcmp0(gst_plugin_feature_get_plugin_name(GST_PLUGIN_FEATURE(factory)), "va") &&
          gst_element_factory_can_src_any_caps(factory, h264))
        g_ptr_array_add(names, g_strdup(GST_OBJECT_NAME(factory)));
    }
    gst_plugin_feature_list_free(factories);
    gst_caps_unref(h264);
  }
  gboolean ok = zen_encoder_select_verified(c, valid, (const char *const *)names->pdata, names->len, zen_encoder_try_factory, NULL, selection, error);
  g_ptr_array_unref(names);
  return ok;
}
