/* xdg-desktop-portal protocol v2 over typed GLib D-Bus, without persistent grants. */
#include "portal.h"
#include <gio/gunixfdlist.h>
#include <math.h>
#include <string.h>
#include <unistd.h>

#define DEST "org.freedesktop.portal.Desktop"
#define PATH "/org/freedesktop/portal/desktop"
#define REMOTE "org.freedesktop.portal.RemoteDesktop"
#define SCREEN "org.freedesktop.portal.ScreenCast"
#define REQUEST "org.freedesktop.portal.Request"
#define SESSION "org.freedesktop.portal.Session"

static gboolean fail(GError **error, GIOErrorEnum code, const char *message) {
  g_set_error_literal(error, G_IO_ERROR, code, message);
  return FALSE;
}

static GVariant *empty_options(void) {
  return g_variant_new_array(G_VARIANT_TYPE("{sv}"), NULL, 0);
}

typedef struct {
  GMainLoop *loop;
  GVariant *results;
  guint32 response;
  gint finished;
} PendingRequest;

static void response(GDBusConnection *bus, const char *sender, const char *path,
                     const char *interface, const char *signal, GVariant *parameters, gpointer data) {
  (void)bus; (void)sender; (void)path; (void)interface; (void)signal;
  PendingRequest *pending = data;
  if (g_atomic_int_get(&pending->finished) || !g_variant_is_of_type(parameters, G_VARIANT_TYPE("(ua{sv})"))) return;
  g_variant_get(parameters, "(u@a{sv})", &pending->response, &pending->results);
  g_atomic_int_set(&pending->finished, TRUE);
  g_main_loop_quit(pending->loop);
}

static gboolean request_timeout(gpointer data) {
  PendingRequest *pending = data;
  g_atomic_int_set(&pending->finished, TRUE);
  g_main_loop_quit(pending->loop);
  return G_SOURCE_REMOVE;
}

static gboolean request_cancelled(GCancellable *cancel, gpointer data) {
  (void)cancel;
  PendingRequest *pending = data;
  g_atomic_int_set(&pending->finished, TRUE);
  g_main_loop_quit(pending->loop);
  return G_SOURCE_REMOVE;
}

static char *owned_path(GDBusConnection *bus, const char *kind, const char *token) {
  const char *unique = g_dbus_connection_get_unique_name(bus);
  if (!unique || unique[0] != ':') return NULL;
  char *sender = g_strdup(unique + 1);
  g_strdelimit(sender, ".", '_');
  char *path = g_strdup_printf("/org/freedesktop/portal/desktop/%s/%s/%s", kind, sender, token);
  g_free(sender);
  return path;
}

/* Subscribe before the method call: a portal may reply before the call returns. */
static GVariant *request(ZenPortal *portal, const char *interface, const char *method,
                         const char *token, GVariant *parameters, GError **error) {
  char *path = owned_path(portal->bus, "request", token);
  if (!path) {
    fail(error, G_IO_ERROR_INVALID_ARGUMENT, "Portal requires a message-bus connection.");
    return NULL;
  }
  GMainContext *context = g_main_context_ref_thread_default();
  PendingRequest pending = { .loop = g_main_loop_new(context, FALSE), .response = 2 };
  guint subscription = g_dbus_connection_signal_subscribe(portal->bus, DEST, REQUEST, "Response", path,
    NULL, G_DBUS_SIGNAL_FLAGS_NONE, response, &pending, NULL);
  GVariant *reply = g_dbus_connection_call_sync(portal->bus, DEST, PATH, interface, method, parameters,
    G_VARIANT_TYPE("(o)"), G_DBUS_CALL_FLAGS_NONE, 5000, portal->cancel, error);
  gboolean valid_path = FALSE;
  if (reply) {
    const char *actual;
    g_variant_get(reply, "(&o)", &actual);
    valid_path = !strcmp(actual, path);
    if (!valid_path) fail(error, G_IO_ERROR_INVALID_DATA, "Portal returned an unexpected request identity.");
    g_variant_unref(reply);
  }
  if (valid_path) {
    GSource *timer = g_timeout_source_new_seconds(60);
    g_source_set_callback(timer, request_timeout, &pending, NULL);
    g_source_attach(timer, context);
    GSource *cancellation = g_cancellable_source_new(portal->cancel);
    g_source_set_callback(cancellation, G_SOURCE_FUNC(request_cancelled), &pending, NULL);
    g_source_attach(cancellation, context);
    if (!g_atomic_int_get(&pending.finished)) g_main_loop_run(pending.loop);
    g_source_destroy(cancellation); g_source_unref(cancellation);
    g_source_destroy(timer); g_source_unref(timer);
  }
  if (!valid_path || !pending.results || pending.response != 0 || g_cancellable_is_cancelled(portal->cancel)) {
    g_dbus_connection_call(portal->bus, DEST, path, REQUEST, "Close", NULL, NULL,
      G_DBUS_CALL_FLAGS_NONE, 1000, NULL, NULL, NULL);
    if (error && !*error) fail(error, G_IO_ERROR_CANCELLED, "Desktop portal request cancelled, declined or timed out.");
    g_clear_pointer(&pending.results, g_variant_unref);
  }
  g_dbus_connection_signal_unsubscribe(portal->bus, subscription);
  g_main_loop_unref(pending.loop);
  g_main_context_unref(context);
  g_free(path);
  return pending.results;
}

static char *options(GVariantBuilder *builder) {
  char *uuid = g_uuid_string_random();
  char *token = g_strdelimit(uuid, "-", '_');
  g_variant_builder_init(builder, G_VARIANT_TYPE_VARDICT);
  g_variant_builder_add(builder, "{sv}", "handle_token", g_variant_new_string(token));
  return token;
}

gboolean zen_portal_parse_grant(GVariant *results, gboolean control, guint32 *node,
                               int *width, int *height, GError **error) {
  guint32 devices = 0;
  if (!results || !g_variant_is_of_type(results, G_VARIANT_TYPE_VARDICT))
    return fail(error, G_IO_ERROR_INVALID_DATA, "Invalid desktop portal grant.");
  if (control && (!g_variant_lookup(results, "devices", "u", &devices) || (devices & 3) != 3))
    return fail(error, G_IO_ERROR_PERMISSION_DENIED, "Portal did not grant both keyboard and pointer control.");
  GVariant *streams = g_variant_lookup_value(results, "streams", G_VARIANT_TYPE("a(ua{sv})"));
  if (!streams || g_variant_n_children(streams) != 1) {
    if (streams) g_variant_unref(streams);
    return fail(error, G_IO_ERROR_INVALID_DATA, "Exactly one selected desktop stream is required.");
  }
  GVariant *stream = g_variant_get_child_value(streams, 0);
  GVariant *properties;
  guint32 selected = 0, source_type = 0;
  int w = 0, h = 0;
  g_variant_get(stream, "(u@a{sv})", &selected, &properties);
  gboolean has_size = g_variant_lookup(properties, "size", "(ii)", &w, &h);
  gboolean monitor = g_variant_lookup(properties, "source_type", "u", &source_type) && source_type == 1;
  g_variant_unref(properties); g_variant_unref(stream); g_variant_unref(streams);
  if (!selected || !has_size || !monitor || w < 2 || h < 2 || w > 16384 || h > 16384)
    return fail(error, G_IO_ERROR_INVALID_DATA, "Portal stream lacks valid monitor identity or logical dimensions.");
  *node = selected; *width = w; *height = h;
  return TRUE;
}

static void session_closed(GDBusConnection *bus, const char *sender, const char *path,
                           const char *interface, const char *signal, GVariant *parameters, gpointer data) {
  (void)bus; (void)sender; (void)path; (void)interface; (void)signal; (void)parameters;
  ZenPortal *portal = data;
  portal->revoked = TRUE;
  g_cancellable_cancel(portal->cancel);
  if (portal->fd >= 0) { close(portal->fd); portal->fd = -1; }
}

void zen_portal_init(ZenPortal *portal, GDBusConnection *bus, GCancellable *cancel) {
  memset(portal, 0, sizeof(*portal));
  portal->fd = -1;
  portal->bus = g_object_ref(bus);
  portal->cancel = cancel ? g_object_ref(cancel) : g_cancellable_new();
  portal->keys = g_hash_table_new(g_direct_hash, g_direct_equal);
}

gboolean zen_portal_open(ZenPortal *portal, gboolean control, const char *parent_window, GError **error) {
  if (portal->session || portal->revoked || g_cancellable_is_cancelled(portal->cancel))
    return fail(error, G_IO_ERROR_CLOSED, "Desktop portal session cannot be reused.");
  GVariantBuilder builder;
  char *token = options(&builder);
  char *expected_session = owned_path(portal->bus, "session", token);
  g_variant_builder_add(&builder, "{sv}", "session_handle_token", g_variant_new_string(token));
  GVariant *result = request(portal, control ? REMOTE : SCREEN, "CreateSession", token,
    g_variant_new("(a{sv})", &builder), error);
  g_free(token);
  if (!result) { g_free(expected_session); return FALSE; }
  const char *session = NULL;
  gboolean valid = g_variant_lookup(result, "session_handle", "&s", &session) &&
    g_variant_is_object_path(session) && !g_strcmp0(session, expected_session);
  g_free(expected_session);
  if (valid) portal->session = g_strdup(session);
  g_variant_unref(result);
  if (!valid) return fail(error, G_IO_ERROR_INVALID_DATA, "Portal returned an invalid session identity.");
  portal->closed_subscription = g_dbus_connection_signal_subscribe(portal->bus, DEST, SESSION, "Closed",
    portal->session, NULL, G_DBUS_SIGNAL_FLAGS_NONE, session_closed, portal, NULL);
  portal->control = control;
  if (control) {
    token = options(&builder);
    g_variant_builder_add(&builder, "{sv}", "types", g_variant_new_uint32(3));
    g_variant_builder_add(&builder, "{sv}", "persist_mode", g_variant_new_uint32(0));
    result = request(portal, REMOTE, "SelectDevices", token,
      g_variant_new("(oa{sv})", portal->session, &builder), error);
    g_free(token);
    if (!result) goto failed;
    g_variant_unref(result);
  }
  token = options(&builder);
  g_variant_builder_add(&builder, "{sv}", "types", g_variant_new_uint32(1));
  g_variant_builder_add(&builder, "{sv}", "multiple", g_variant_new_boolean(FALSE));
  g_variant_builder_add(&builder, "{sv}", "cursor_mode", g_variant_new_uint32(2));
  if (!control) g_variant_builder_add(&builder, "{sv}", "persist_mode", g_variant_new_uint32(0));
  result = request(portal, SCREEN, "SelectSources", token,
    g_variant_new("(oa{sv})", portal->session, &builder), error);
  g_free(token);
  if (!result) goto failed;
  g_variant_unref(result);
  token = options(&builder);
  result = request(portal, control ? REMOTE : SCREEN, "Start", token,
    g_variant_new("(osa{sv})", portal->session, parent_window ? parent_window : "", &builder), error);
  g_free(token);
  if (!result) goto failed;
  valid = zen_portal_parse_grant(result, control, &portal->node, &portal->width, &portal->height, error);
  g_variant_unref(result);
  if (!valid) goto failed;
  GUnixFDList *fds = NULL;
  result = g_dbus_connection_call_with_unix_fd_list_sync(portal->bus, DEST, PATH, SCREEN, "OpenPipeWireRemote",
    g_variant_new("(o@a{sv})", portal->session, empty_options()), G_VARIANT_TYPE("(h)"),
    G_DBUS_CALL_FLAGS_NONE, 5000, NULL, &fds, portal->cancel, error);
  if (!result) goto failed;
  gint handle;
  g_variant_get(result, "(h)", &handle);
  g_variant_unref(result);
  if (fds) { portal->fd = g_unix_fd_list_get(fds, handle, error); g_object_unref(fds); }
  else fail(error, G_IO_ERROR_INVALID_DATA, "Portal did not provide a PipeWire descriptor.");
  if (portal->fd < 0) goto failed;
  return TRUE;
failed:
  zen_portal_close(portal);
  return FALSE;
}

GVariant *zen_portal_input_parameters(const char *session, guint32 node, int width, int height,
                                     const char *type, double x, double y, guint32 code,
                                     gboolean down, int delta, const char **method, GError **error) {
  *method = NULL;
  if (!session || !g_variant_is_object_path(session) || !type) goto invalid;
  if (!strcmp(type, "pointer")) {
    if (!node || width < 2 || height < 2 || !isfinite(x) || !isfinite(y) || x < 0 || x > 1 || y < 0 || y > 1) goto invalid;
    *method = "NotifyPointerMotionAbsolute";
    return g_variant_new("(o@a{sv}udd)", session, empty_options(), node, x * (width - 1), y * (height - 1));
  }
  if (!strcmp(type, "button")) {
    if (code < 1 || code > 3) goto invalid;
    const gint evdev[] = {0, 0x110, 0x112, 0x111};
    *method = "NotifyPointerButton";
    return g_variant_new("(o@a{sv}iu)", session, empty_options(), evdev[code], down ? 1u : 0u);
  }
  if (!strcmp(type, "key")) {
    if (!((code >= 0x20 && code <= 0x7e) || (code >= 0xff08 && code <= 0xffff))) goto invalid;
    *method = "NotifyKeyboardKeysym";
    return g_variant_new("(o@a{sv}iu)", session, empty_options(), (gint)code, down ? 1u : 0u);
  }
  if (!strcmp(type, "scroll")) {
    if (delta < -10 || delta > 10) goto invalid;
    *method = "NotifyPointerAxisDiscrete";
    return g_variant_new("(o@a{sv}ui)", session, empty_options(), 0u, delta);
  }
invalid:
  fail(error, G_IO_ERROR_INVALID_ARGUMENT, "Invalid desktop portal input.");
  return NULL;
}

static gboolean send_input(ZenPortal *portal, const char *type, double x, double y,
                           guint32 code, gboolean down, int delta, int timeout, GError **error) {
  const char *method;
  GVariant *parameters = zen_portal_input_parameters(portal->session, portal->node, portal->width,
    portal->height, type, x, y, code, down, delta, &method, error);
  if (!parameters) return FALSE;
  GVariant *reply = g_dbus_connection_call_sync(portal->bus, DEST, PATH, REMOTE, method,
    parameters, G_VARIANT_TYPE_UNIT, G_DBUS_CALL_FLAGS_NONE, timeout, portal->cancel, error);
  if (!reply) { zen_portal_close(portal); return FALSE; }
  g_variant_unref(reply);
  if (!strcmp(type, "key")) {
    if (down) g_hash_table_add(portal->keys, GUINT_TO_POINTER(code));
    else g_hash_table_remove(portal->keys, GUINT_TO_POINTER(code));
  } else if (!strcmp(type, "button")) portal->buttons[code] = down;
  return TRUE;
}

gboolean zen_portal_input(ZenPortal *portal, const char *type, double x, double y,
                          guint32 code, gboolean down, int delta, GError **error) {
  if (!portal->bus || !portal->control || !portal->session || portal->revoked || portal->fd < 0)
    return fail(error, G_IO_ERROR_PERMISSION_DENIED, "Desktop control has not been granted or was revoked.");
  if (type && !strcmp(type, "release")) {
    gint64 deadline = g_get_monotonic_time() + G_TIME_SPAN_SECOND;
    GList *held = g_hash_table_get_keys(portal->keys);
    gboolean ok = TRUE;
    for (GList *key = held; key && ok; key = key->next) {
      int remaining = (int)((deadline - g_get_monotonic_time()) / 1000);
      if (remaining <= 0) { ok = fail(error, G_IO_ERROR_TIMED_OUT, "Portal release timed out."); break; }
      ok = send_input(portal, "key", 0, 0, GPOINTER_TO_UINT(key->data), FALSE, 0, remaining, error);
    }
    g_list_free(held);
    for (guint code_index = 1; code_index <= 3 && ok; code_index++) {
      if (!portal->buttons[code_index]) continue;
      int remaining = (int)((deadline - g_get_monotonic_time()) / 1000);
      if (remaining <= 0) { ok = fail(error, G_IO_ERROR_TIMED_OUT, "Portal release timed out."); break; }
      ok = send_input(portal, "button", 0, 0, code_index, FALSE, 0, remaining, error);
    }
    if (!ok) zen_portal_close(portal);
    return ok;
  }
  if (type && !strcmp(type, "key") && down && !g_hash_table_contains(portal->keys, GUINT_TO_POINTER(code)) &&
      g_hash_table_size(portal->keys) >= 32) {
    zen_portal_close(portal);
    return fail(error, G_IO_ERROR_NO_SPACE, "Too many held desktop keys.");
  }
  return send_input(portal, type, x, y, code, down, delta, 1000, error);
}

void zen_portal_close(ZenPortal *portal) {
  /* Closing the portal session revokes its virtual devices, including held input. */
  if (portal->session && portal->bus) {
    GVariant *reply = g_dbus_connection_call_sync(portal->bus, DEST, portal->session, SESSION, "Close",
      NULL, G_VARIANT_TYPE_UNIT, G_DBUS_CALL_FLAGS_NONE, 1000, NULL, NULL);
    if (reply) g_variant_unref(reply);
  }
  if (portal->closed_subscription && portal->bus)
    g_dbus_connection_signal_unsubscribe(portal->bus, portal->closed_subscription);
  if (portal->fd >= 0) close(portal->fd);
  g_clear_object(&portal->bus);
  g_clear_object(&portal->cancel);
  g_clear_pointer(&portal->session, g_free);
  g_clear_pointer(&portal->keys, g_hash_table_unref);
  memset(portal, 0, sizeof(*portal));
  portal->fd = -1; portal->revoked = TRUE;
}
