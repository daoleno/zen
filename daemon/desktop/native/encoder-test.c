#include "encoder.h"
#include <gst/app/gstappsink.h>

static ZenEncoderConfig config(void) {
  return (ZenEncoderConfig){ .width = 640, .height = 360, .fps = 30, .bitrate = 4000000 };
}

static void bounds(void) {
  ZenEncoderConfig c = config();
  g_assert_true(zen_encoder_config_valid(&c));
  c.fps = 60; g_assert_false(zen_encoder_config_valid(&c));
  c = config(); c.width = 1920; g_assert_false(zen_encoder_config_valid(&c));
  c = config(); c.height = 359; g_assert_false(zen_encoder_config_valid(&c));
  const char *invalid[] = {NULL, "", "/dev/dri/card0", "/dev/null", "/dev/dri/renderD999", "/dev/dri/renderD128/../card0"};
  for (guint i = 0; i < G_N_ELEMENTS(invalid); i++) g_assert_false(zen_encoder_render_node_valid(invalid[i]));
}

static void caps(void) {
  ZenEncoderConfig c = config();
  GstCaps *sink = gst_caps_from_string("video/x-raw,format=(string){NV12,I420},width=(int)[2,1920],height=(int)[2,1080],framerate=(fraction)[1/1,60/1]");
  GstCaps *src = gst_caps_from_string("video/x-h264,stream-format=byte-stream,alignment=au,profile=constrained-baseline");
  g_assert_true(zen_encoder_caps_supported(&c, sink, src, TRUE));
  GstCaps *wrong = gst_caps_from_string("video/x-h264,profile=main");
  g_assert_false(zen_encoder_caps_supported(&c, sink, wrong, TRUE)); gst_caps_unref(wrong);
  wrong = gst_caps_from_string("video/x-raw(memory:VAMemory),format=NV12");
  g_assert_false(zen_encoder_caps_supported(&c, wrong, src, TRUE)); gst_caps_unref(wrong);
  wrong = gst_caps_from_string("video/x-raw,format=I420");
  g_assert_false(zen_encoder_caps_supported(&c, wrong, src, TRUE));
  g_assert_true(zen_encoder_caps_supported(&c, wrong, src, FALSE)); gst_caps_unref(wrong);
  wrong = gst_caps_new_any();
  g_assert_false(zen_encoder_caps_supported(&c, wrong, src, TRUE)); gst_caps_unref(wrong);
  wrong = gst_caps_new_empty();
  g_assert_false(zen_encoder_caps_supported(&c, sink, wrong, TRUE)); gst_caps_unref(wrong);
  gst_caps_unref(sink); gst_caps_unref(src);
}

typedef struct { guint hardware, software; gboolean software_fails, cancel; } Attempts;
static GstElement *fake_attempt(const ZenEncoderConfig *c, const char *name, gboolean hardware, gpointer data, GError **error) {
  Attempts *a = data;
  if (hardware) a->hardware++; else a->software++;
  if (a->cancel) g_cancellable_cancel(c->cancel);
  if ((hardware && !g_str_equal(name, "good")) || (!hardware && a->software_fails) || a->cancel) {
    g_set_error_literal(error, G_IO_ERROR, G_IO_ERROR_NOT_SUPPORTED, "deterministic_probe_failure"); return NULL;
  }
  return gst_bin_new(NULL);
}

static void selection(void) {
  const char *names[] = {"bad", "good"};
  ZenEncoderConfig c = config(); ZenEncoderSelection selected; GError *error = NULL;
  Attempts a = {0};
  g_assert_true(zen_encoder_select_verified(&c, TRUE, names, 2, fake_attempt, &a, &selected, &error));
  g_assert_cmpuint(a.hardware, ==, 0); g_assert_false(selected.hardware);
  g_assert_cmpstr(selected.reason, ==, "software_default"); gst_object_unref(selected.bin);
  c.render_node = "/dev/dri/renderD128"; a = (Attempts){0};
  g_assert_true(zen_encoder_select_verified(&c, FALSE, names, 2, fake_attempt, &a, &selected, &error));
  g_assert_cmpuint(a.hardware, ==, 0); g_assert_cmpstr(selected.reason, ==, "invalid_render_node"); gst_object_unref(selected.bin);
  a = (Attempts){0};
  g_assert_true(zen_encoder_select_verified(&c, TRUE, names, 2, fake_attempt, &a, &selected, &error));
  g_assert_cmpuint(a.hardware, ==, 2); g_assert_cmpuint(a.software, ==, 0);
  g_assert_true(selected.hardware); gst_object_unref(selected.bin);
  a = (Attempts){0};
  g_assert_true(zen_encoder_select_verified(&c, TRUE, names, 1, fake_attempt, &a, &selected, &error));
  g_assert_cmpstr(selected.reason, ==, "hardware_probe_failed"); g_assert_false(selected.hardware);
  g_assert_cmpuint(a.software, ==, 1); gst_object_unref(selected.bin);
  a = (Attempts){ .software_fails = TRUE };
  g_assert_false(zen_encoder_select_verified(&c, TRUE, names, 1, fake_attempt, &a, &selected, &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_NOT_SUPPORTED); g_clear_error(&error);
  c.cancel = g_cancellable_new(); a = (Attempts){ .cancel = TRUE };
  g_assert_false(zen_encoder_select_verified(&c, TRUE, names, 2, fake_attempt, &a, &selected, &error));
  g_assert_cmpuint(a.software, ==, 0); g_assert_error(error, G_IO_ERROR, G_IO_ERROR_CANCELLED);
  g_clear_error(&error); g_object_unref(c.cancel);
}

static gboolean cancel_idle(gpointer cancel) { g_cancellable_cancel(cancel); return G_SOURCE_REMOVE; }
static gboolean busy_idle(gpointer unused) { (void)unused; return G_SOURCE_CONTINUE; }

static void probe_failures(void) {
  ZenEncoderConfig c = config(); GError *error = NULL;
  c.render_node = "/dev/dri/renderD128";
  g_assert_null(zen_encoder_try_factory(&c, "openh264enc", TRUE, NULL, &error));
  g_assert_cmpstr(error->message, ==, "hardware_device_property_missing"); g_clear_error(&error);
  g_assert_null(zen_encoder_try_factory(&c, "zen-missing-encoder", FALSE, NULL, &error));
  g_assert_cmpstr(error->message, ==, "encoder_plugin_unavailable"); g_clear_error(&error);
  g_assert_false(zen_encoder_probe(gst_element_factory_make("identity", NULL), &c, &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_NOT_SUPPORTED); g_clear_error(&error);
  GstElement *valve = gst_element_factory_make("valve", NULL);
  g_object_set(valve, "drop", TRUE, NULL);
  guint busy = g_idle_add(busy_idle, NULL);
  gint64 start = g_get_monotonic_time();
  g_assert_false(zen_encoder_probe(valve, &c, &error));
  g_source_remove(busy);
  g_assert_cmpint(g_get_monotonic_time() - start, <, 3 * G_TIME_SPAN_SECOND); g_clear_error(&error);
  c.cancel = g_cancellable_new(); g_idle_add(cancel_idle, c.cancel);
  g_assert_false(zen_encoder_probe(gst_element_factory_make("identity", NULL), &c, &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_CANCELLED); g_clear_error(&error); g_object_unref(c.cancel);
}

static void software_pipeline(void) {
  ZenEncoderConfig c = config(); ZenEncoderSelection selected; GError *error = NULL;
  c.render_node = "/dev/dri/card0";
  g_assert_true(zen_encoder_select(&c, &selected, &error)); g_assert_no_error(error);
  g_assert_false(selected.hardware); g_assert_cmpstr(selected.reason, ==, "invalid_render_node");
  GstElement *codec = gst_bin_get_by_name(GST_BIN(selected.bin), "codec");
  guint bitrate, gop;
  g_object_get(codec, "bitrate", &bitrate, "gop-size", &gop, NULL);
  g_assert_cmpuint(bitrate, ==, c.bitrate); g_assert_cmpuint(gop, ==, c.fps); gst_object_unref(codec);
  GstElement *pipeline = gst_pipeline_new(NULL), *source = gst_element_factory_make("videotestsrc", NULL);
  GstElement *decoder = gst_element_factory_make("openh264dec", NULL), *sink = gst_element_factory_make("appsink", NULL);
  g_assert_nonnull(decoder);
  gst_bin_add_many(GST_BIN(pipeline), source, selected.bin, decoder, sink, NULL);
  g_object_set(source, "num-buffers", 24, NULL); gst_util_set_object_arg(G_OBJECT(source), "pattern", "ball");
  g_object_set(sink, "sync", FALSE, "max-buffers", 2u, "drop", FALSE, NULL);
  g_assert_true(gst_element_link_many(source, selected.bin, decoder, sink, NULL));
  g_assert_cmpint(gst_element_set_state(pipeline, GST_STATE_PLAYING), !=, GST_STATE_CHANGE_FAILURE);
  guint frames = 0; gchar *first = NULL; gboolean changed = FALSE;
  gint64 deadline = g_get_monotonic_time() + 5 * G_TIME_SPAN_SECOND;
  while (frames < 24 && g_get_monotonic_time() < deadline) {
    GstSample *sample = gst_app_sink_try_pull_sample(GST_APP_SINK(sink), 100 * GST_MSECOND);
    if (!sample) continue;
    const GstStructure *caps = gst_caps_get_structure(gst_sample_get_caps(sample), 0);
    gint width, height;
    g_assert_true(gst_structure_get_int(caps, "width", &width)); g_assert_cmpint(width, ==, c.width);
    g_assert_true(gst_structure_get_int(caps, "height", &height)); g_assert_cmpint(height, ==, c.height);
    GstMapInfo map; g_assert_true(gst_buffer_map(gst_sample_get_buffer(sample), &map, GST_MAP_READ));
    gchar *hash = g_compute_checksum_for_data(G_CHECKSUM_SHA256, map.data, map.size);
    if (!first) first = g_strdup(hash); else if (!g_str_equal(first, hash)) changed = TRUE;
    g_free(hash); gst_buffer_unmap(gst_sample_get_buffer(sample), &map); gst_sample_unref(sample); frames++;
  }
  g_assert_cmpuint(frames, ==, 24); g_assert_true(changed); g_free(first);
  gst_element_set_state(pipeline, GST_STATE_NULL); gst_object_unref(pipeline);
}

int main(int argc, char **argv) {
  g_test_init(&argc, &argv, NULL);
  g_setenv("GST_REGISTRY", "/nonexistent/zen-encoder-test-registry", TRUE);
  g_setenv("GST_REGISTRY_UPDATE", "no", TRUE);
  gst_init(NULL, NULL);
  const char *names[] = {"coreelements", "videotestsrc", "videoconvertscale", "openh264", "videoparsersbad", "app"};
  for (guint i = 0; i < G_N_ELEMENTS(names); i++) {
    gchar *path = g_strdup_printf("%s/libgst%s.so", g_getenv("ZEN_ENCODER_TEST_PLUGINS"), names[i]);
    GError *error = NULL; GstPlugin *plugin = gst_plugin_load_file(path, &error);
    g_assert_no_error(error); g_assert_nonnull(plugin); gst_object_unref(plugin); g_free(path);
  }
  g_test_add_func("/encoder/configuration", bounds);
  g_test_add_func("/encoder/caps", caps);
  g_test_add_func("/encoder/selection", selection);
  g_test_add_func("/encoder/probe-failures", probe_failures);
  g_test_add_func("/encoder/software-pipeline", software_pipeline);
  return g_test_run();
}
