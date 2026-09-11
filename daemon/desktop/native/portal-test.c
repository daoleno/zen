#include "portal.h"
#include "portal_capture.h"
#include <fcntl.h>
#include <math.h>
#include <unistd.h>

#define DEST "org.freedesktop.portal.Desktop"
#define PATH "/org/freedesktop/portal/desktop"
#define REMOTE "org.freedesktop.portal.RemoteDesktop"
#define SCREEN "org.freedesktop.portal.ScreenCast"
#define SESSION "/org/freedesktop/portal/desktop/session/mock/session"

static GVariant *grant(guint devices, guint count, guint source_type, int width, int height) {
  GVariantBuilder result, streams;
  g_variant_builder_init(&result, G_VARIANT_TYPE_VARDICT);
  g_variant_builder_init(&streams, G_VARIANT_TYPE("a(ua{sv})"));
  for (guint i = 0; i < count; i++) {
    GVariantBuilder props;
    g_variant_builder_init(&props, G_VARIANT_TYPE_VARDICT);
    g_variant_builder_add(&props, "{sv}", "size", g_variant_new("(ii)", width, height));
    g_variant_builder_add(&props, "{sv}", "source_type", g_variant_new_uint32(source_type));
    g_variant_builder_add(&streams, "(ua{sv})", 17u + i, &props);
  }
  g_variant_builder_add(&result, "{sv}", "devices", g_variant_new_uint32(devices));
  g_variant_builder_add(&result, "{sv}", "streams", g_variant_builder_end(&streams));
  return g_variant_ref_sink(g_variant_builder_end(&result));
}

static void grant_bounds(void) {
  guint node = 0;
  int width = 0, height = 0;
  const int invalid[][5] = {{1,1,1,640,480}, {3,0,1,640,480}, {3,2,1,640,480},
                            {3,1,2,640,480}, {3,1,1,0,480}, {3,1,1,20000,480}};
  for (guint i = 0; i < G_N_ELEMENTS(invalid); i++) {
    GError *error = NULL;
    GVariant *value = grant(invalid[i][0], invalid[i][1], invalid[i][2], invalid[i][3], invalid[i][4]);
    g_assert_false(zen_portal_parse_grant(value, TRUE, &node, &width, &height, &error));
    g_assert_nonnull(error); g_clear_error(&error); g_variant_unref(value);
  }
  GError *error = NULL;
  GVariant *value = grant(3, 1, 1, 640, 480);
  g_assert_true(zen_portal_parse_grant(value, TRUE, &node, &width, &height, &error));
  g_assert_no_error(error); g_assert_cmpuint(node, ==, 17);
  g_assert_cmpint(width, ==, 640); g_assert_cmpint(height, ==, 480);
  g_variant_unref(value);
}

static void input_contract(void) {
  GError *error = NULL;
  const char *method, *session;
  GVariant *opts;
  guint node;
  double x, y;
  GVariant *value = zen_portal_input_parameters(SESSION, 17, 640, 480, "pointer", 1, .5, 0, FALSE, 0, &method, &error);
  g_assert_no_error(error); g_assert_cmpstr(method, ==, "NotifyPointerMotionAbsolute");
  g_variant_get(value, "(&o@a{sv}udd)", &session, &opts, &node, &x, &y);
  g_assert_cmpuint(node, ==, 17); g_assert_cmpfloat(x, ==, 639); g_assert_cmpfloat(y, ==, 239.5);
  g_variant_unref(opts); g_variant_unref(g_variant_ref_sink(value));
  for (guint button = 1; button <= 3; button++) {
    gint evdev; guint pressed;
    value = zen_portal_input_parameters(SESSION, 17, 640, 480, "button", 0, 0, button, TRUE, 0, &method, &error);
    g_variant_get(value, "(&o@a{sv}iu)", &session, &opts, &evdev, &pressed);
    const int expected[] = {0,0x110,0x112,0x111};
    g_assert_cmpint(evdev, ==, expected[button]); g_assert_cmpuint(pressed, ==, 1);
    g_variant_unref(opts); g_variant_unref(g_variant_ref_sink(value));
  }
  value = zen_portal_input_parameters(SESSION, 17, 640, 480, "pointer", NAN, 0, 0, FALSE, 0, &method, &error);
  g_assert_null(value); g_assert_error(error, G_IO_ERROR, G_IO_ERROR_INVALID_ARGUMENT); g_clear_error(&error);
}

typedef struct {
  GTestDBus *test_bus;
  GDBusConnection *server;
  GDBusNodeInfo *info;
  GMainContext *context;
  GMainLoop *loop;
  GThread *thread;
  GMutex mutex;
  GCond ready;
  gboolean running;
  gint deny, wrong_session, hold_start, held_starts, unsupported_control, devices;
  gint input_count, close_count;
} Mock;

static void method_call(GDBusConnection *bus, const char *sender, const char *path, const char *interface,
                         const char *method, GVariant *parameters, GDBusMethodInvocation *invocation, gpointer data) {
  (void)path; (void)interface;
  Mock *mock = data;
  if (!strcmp(interface, REMOTE) && !strcmp(method, "CreateSession") && g_atomic_int_get(&mock->unsupported_control)) {
    g_dbus_method_invocation_return_error(invocation, G_DBUS_ERROR, G_DBUS_ERROR_UNKNOWN_METHOD, "RemoteDesktop is unavailable");
    return;
  }
  if (!strcmp(method, "Close")) {
    g_atomic_int_inc(&mock->close_count);
    g_dbus_method_invocation_return_value(invocation, NULL);
    return;
  }
  if (g_str_has_prefix(method, "Notify")) {
    g_atomic_int_inc(&mock->input_count);
    g_dbus_method_invocation_return_value(invocation, NULL);
    return;
  }
  if (!strcmp(method, "OpenPipeWireRemote")) {
    int pipes[2]; g_assert_cmpint(pipe(pipes), ==, 0);
    GUnixFDList *fds = g_unix_fd_list_new();
    gint index = g_unix_fd_list_append(fds, pipes[0], NULL);
    g_dbus_method_invocation_return_value_with_unix_fd_list(invocation, g_variant_new("(h)", index), fds);
    close(pipes[0]); close(pipes[1]); g_object_unref(fds);
    return;
  }
  GVariant *options = g_variant_get_child_value(parameters, g_variant_n_children(parameters)-1);
  const char *token;
  g_assert_true(g_variant_lookup(options, "handle_token", "&s", &token));
  char *identity = g_strdup(sender + 1); g_strdelimit(identity, ".", '_');
  char *request_path = g_strdup_printf("/org/freedesktop/portal/desktop/request/%s/%s", identity, token);
  g_free(identity);
  if (!strcmp(method, "SelectDevices")) {
    guint types, persist;
    g_assert_true(g_variant_lookup(options, "types", "u", &types)); g_assert_cmpuint(types, ==, 3);
    g_assert_true(g_variant_lookup(options, "persist_mode", "u", &persist)); g_assert_cmpuint(persist, ==, 0);
    g_assert_null(g_variant_lookup_value(options, "restore_token", NULL));
  }
  GVariant *result;
  if (!strcmp(method, "Start")) result = grant(g_atomic_int_get(&mock->devices), 1, 1, 640, 480);
  else {
    GVariantBuilder builder; g_variant_builder_init(&builder, G_VARIANT_TYPE_VARDICT);
    if (!strcmp(method, "CreateSession")) {
      const char *session_token;
      g_assert_true(g_variant_lookup(options, "session_handle_token", "&s", &session_token));
      char *owner = g_strdup(sender + 1); g_strdelimit(owner, ".", '_');
      char *session_path = g_strdup_printf("/org/freedesktop/portal/desktop/session/%s/%s", owner, session_token);
      g_free(owner);
      static GDBusInterfaceVTable session_table = { .method_call = method_call };
      g_assert_cmpuint(g_dbus_connection_register_object(bus, session_path, mock->info->interfaces[2],
        &session_table, mock, NULL, NULL), >, 0);
      g_variant_builder_add(&builder, "{sv}", "session_handle", g_variant_new_string(
        g_atomic_int_get(&mock->wrong_session) ? SESSION : session_path));
      g_free(session_path);
    }
    result = g_variant_ref_sink(g_variant_builder_end(&builder));
  }
  g_dbus_method_invocation_return_value(invocation, g_variant_new("(o)", request_path));
  if (g_atomic_int_get(&mock->hold_start) && !strcmp(method, "Start")) {
    g_atomic_int_inc(&mock->held_starts); g_variant_unref(result);
  }
  else g_dbus_connection_emit_signal(bus, sender, request_path, "org.freedesktop.portal.Request", "Response",
    g_variant_new("(u@a{sv})", g_atomic_int_get(&mock->deny) && !strcmp(method, "Start") ? 1u : 0u, result), NULL);
  g_variant_unref(options); g_free(request_path);
}

static const char *xml =
  "<node><interface name='org.freedesktop.portal.RemoteDesktop'>"
  "<method name='CreateSession'><arg type='a{sv}' direction='in'/><arg type='o' direction='out'/></method>"
  "<method name='SelectDevices'><arg type='o' direction='in'/><arg type='a{sv}' direction='in'/><arg type='o' direction='out'/></method>"
  "<method name='Start'><arg type='o' direction='in'/><arg type='s' direction='in'/><arg type='a{sv}' direction='in'/><arg type='o' direction='out'/></method>"
  "<method name='NotifyKeyboardKeysym'><arg type='o' direction='in'/><arg type='a{sv}' direction='in'/><arg type='i' direction='in'/><arg type='u' direction='in'/></method>"
  "<method name='NotifyPointerButton'><arg type='o' direction='in'/><arg type='a{sv}' direction='in'/><arg type='i' direction='in'/><arg type='u' direction='in'/></method>"
  "<method name='NotifyPointerMotionAbsolute'><arg type='o' direction='in'/><arg type='a{sv}' direction='in'/><arg type='u' direction='in'/><arg type='d' direction='in'/><arg type='d' direction='in'/></method>"
  "<method name='NotifyPointerAxisDiscrete'><arg type='o' direction='in'/><arg type='a{sv}' direction='in'/><arg type='u' direction='in'/><arg type='i' direction='in'/></method>"
  "</interface><interface name='org.freedesktop.portal.ScreenCast'>"
  "<method name='CreateSession'><arg type='a{sv}' direction='in'/><arg type='o' direction='out'/></method>"
  "<method name='Start'><arg type='o' direction='in'/><arg type='s' direction='in'/><arg type='a{sv}' direction='in'/><arg type='o' direction='out'/></method>"
  "<method name='SelectSources'><arg type='o' direction='in'/><arg type='a{sv}' direction='in'/><arg type='o' direction='out'/></method>"
  "<method name='OpenPipeWireRemote'><arg type='o' direction='in'/><arg type='a{sv}' direction='in'/><arg type='h' direction='out'/></method>"
  "</interface><interface name='org.freedesktop.portal.Session'><method name='Close'/></interface></node>";

static gpointer mock_thread(gpointer data) {
  Mock *mock = data;
  g_main_context_push_thread_default(mock->context);
  GError *error = NULL;
  mock->server = g_dbus_connection_new_for_address_sync(g_test_dbus_get_bus_address(mock->test_bus),
    G_DBUS_CONNECTION_FLAGS_AUTHENTICATION_CLIENT | G_DBUS_CONNECTION_FLAGS_MESSAGE_BUS_CONNECTION, NULL, NULL, &error);
  g_assert_no_error(error);
  GVariant *reply = g_dbus_connection_call_sync(mock->server, "org.freedesktop.DBus", "/org/freedesktop/DBus",
    "org.freedesktop.DBus", "RequestName", g_variant_new("(su)", DEST, 0u), NULL, 0, 1000, NULL, &error);
  g_assert_no_error(error); g_variant_unref(reply);
  GDBusNodeInfo *info = g_dbus_node_info_new_for_xml(xml, &error); g_assert_no_error(error);
  mock->info = info;
  GDBusInterfaceVTable table = { .method_call = method_call };
  for (int i = 0; i < 3; i++) {
    g_assert_cmpuint(g_dbus_connection_register_object(mock->server, i == 2 ? SESSION : PATH,
      info->interfaces[i], &table, mock, NULL, &error), >, 0);
    g_assert_no_error(error);
  }
  g_mutex_lock(&mock->mutex); mock->running = TRUE; g_cond_signal(&mock->ready); g_mutex_unlock(&mock->mutex);
  g_main_loop_run(mock->loop);
  g_dbus_connection_close_sync(mock->server, NULL, NULL);
  g_object_unref(mock->server); g_dbus_node_info_unref(info);
  g_main_context_pop_thread_default(mock->context);
  return NULL;
}

static gboolean cancel_pending(gpointer data) {
  g_cancellable_cancel(G_CANCELLABLE(data));
  return G_SOURCE_REMOVE;
}

static void assert_capture_binding(ZenPortal *portal) {
  GstElement *source = gst_element_factory_make("pipewiresrc", NULL);
  g_assert_nonnull(source);
  GError *error = NULL;
  g_assert_true(zen_portal_bind_source(portal, source, &error)); g_assert_no_error(error);
  int fd = -1, disconnect = 0, buffers = 0, keepalive = -1;
  gboolean resend = TRUE;
  char *path = NULL;
  g_object_get(source, "fd", &fd, "path", &path, "on-disconnect", &disconnect,
    "max-buffers", &buffers, "keepalive-time", &keepalive, "resend-last", &resend, NULL);
  g_assert_cmpint(fd, ==, portal->fd); g_assert_cmpstr(path, ==, "17");
  g_assert_cmpint(disconnect, ==, 2); g_assert_cmpint(buffers, ==, 2);
  g_assert_cmpint(keepalive, ==, 0); g_assert_false(resend);
  // This inspects properties only: the pipe fixture is not a fake video source.
  g_assert_cmpint(GST_STATE(source), ==, GST_STATE_NULL);
  gst_object_unref(source); g_free(path);
  g_assert_cmpint(fcntl(portal->fd, F_GETFD), >=, 0);
}

static void assert_no_capture_binding(ZenPortal *portal) {
  GstElement *source = gst_element_factory_make("pipewiresrc", NULL);
  GError *error = NULL;
  g_assert_false(zen_portal_bind_source(portal, source, &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_PERMISSION_DENIED); g_clear_error(&error);
  gst_object_unref(source);
}

static void lifecycle(void) {
  Mock mock = { .devices = 3 };
  g_mutex_init(&mock.mutex); g_cond_init(&mock.ready);
  mock.test_bus = g_test_dbus_new(G_TEST_DBUS_NONE); g_test_dbus_up(mock.test_bus);
  mock.context = g_main_context_new(); mock.loop = g_main_loop_new(mock.context, FALSE);
  mock.thread = g_thread_new("owned-portal-mock", mock_thread, &mock);
  g_mutex_lock(&mock.mutex); while (!mock.running) g_cond_wait(&mock.ready, &mock.mutex); g_mutex_unlock(&mock.mutex);
  GError *error = NULL;
  GDBusConnection *client = g_dbus_connection_new_for_address_sync(g_test_dbus_get_bus_address(mock.test_bus),
    G_DBUS_CONNECTION_FLAGS_AUTHENTICATION_CLIENT | G_DBUS_CONNECTION_FLAGS_MESSAGE_BUS_CONNECTION, NULL, NULL, &error);
  g_assert_no_error(error);
  ZenPortal portal;
  zen_portal_init(&portal, client, NULL);
  assert_no_capture_binding(&portal);
  g_assert_false(zen_portal_input(&portal, "key", 0, 0, 97, TRUE, 0, &error)); g_clear_error(&error);
  g_assert_true(zen_portal_open(&portal, TRUE, "", &error)); g_assert_no_error(error);
  assert_capture_binding(&portal);
  int fd = portal.fd;
  g_assert_cmpint(fcntl(fd, F_GETFD), >=, 0);
  g_assert_true(zen_portal_input(&portal, "key", 0, 0, 97, TRUE, 0, &error));
  g_assert_true(zen_portal_input(&portal, "button", 0, 0, 1, TRUE, 0, &error));
  g_assert_true(zen_portal_input(&portal, "release", 0, 0, 0, FALSE, 0, &error)); g_assert_no_error(error);
  g_assert_true(zen_portal_input(&portal, "pointer", .5, .5, 0, FALSE, 0, &error)); g_assert_no_error(error);
  g_assert_true(zen_portal_input(&portal, "scroll", 0, 0, 0, FALSE, 3, &error)); g_assert_no_error(error);
  g_assert_cmpint(g_atomic_int_get(&mock.input_count), ==, 6);
  zen_portal_close(&portal); zen_portal_close(&portal);
  g_assert_cmpint(fcntl(fd, F_GETFD), ==, -1);
  g_assert_cmpint(g_atomic_int_get(&mock.close_count), ==, 1);
  g_assert_false(zen_portal_input(&portal, "key", 0, 0, 97, TRUE, 0, &error)); g_clear_error(&error);
  g_atomic_int_set(&mock.deny, TRUE);
  zen_portal_init(&portal, client, NULL);
  g_assert_false(zen_portal_open(&portal, TRUE, "", &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_CANCELLED); g_clear_error(&error);
  g_assert_cmpint(portal.fd, ==, -1); zen_portal_close(&portal);
  assert_no_capture_binding(&portal);
  g_atomic_int_set(&mock.deny, FALSE);
  g_atomic_int_set(&mock.devices, 1);
  zen_portal_init(&portal, client, NULL);
  g_assert_false(zen_portal_open(&portal, TRUE, "", &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_PERMISSION_DENIED); g_clear_error(&error);
  assert_no_capture_binding(&portal); zen_portal_close(&portal);
  g_atomic_int_set(&mock.devices, 3);
  g_atomic_int_set(&mock.unsupported_control, TRUE);
  zen_portal_init(&portal, client, NULL);
  g_assert_false(zen_portal_open(&portal, TRUE, "", &error));
  g_assert_nonnull(error); g_clear_error(&error);
  assert_no_capture_binding(&portal); zen_portal_close(&portal);
  g_atomic_int_set(&mock.unsupported_control, FALSE);
  zen_portal_init(&portal, client, NULL);
  g_assert_true(zen_portal_open(&portal, FALSE, "", &error)); g_assert_no_error(error);
  assert_capture_binding(&portal);
  g_assert_false(zen_portal_input(&portal, "key", 0, 0, 97, TRUE, 0, &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_PERMISSION_DENIED); g_clear_error(&error);
  zen_portal_close(&portal);
  zen_portal_init(&portal, client, NULL);
  g_assert_true(zen_portal_open(&portal, TRUE, "", &error)); g_assert_no_error(error);
  g_dbus_connection_emit_signal(mock.server, NULL, portal.session, "org.freedesktop.portal.Session", "Closed",
    g_variant_new("(a{sv})", NULL), NULL);
  gint64 deadline = g_get_monotonic_time() + G_TIME_SPAN_SECOND;
  while (!portal.revoked && g_get_monotonic_time() < deadline) {
    while (g_main_context_iteration(NULL, FALSE)) {}
    g_usleep(1000);
  }
  g_assert_true(portal.revoked); g_assert_cmpint(portal.fd, ==, -1);
  GstElement *source = gst_element_factory_make("pipewiresrc", NULL);
  g_assert_false(zen_portal_bind_source(&portal, source, &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_PERMISSION_DENIED); g_clear_error(&error);
  gst_object_unref(source);
  g_assert_false(zen_portal_input(&portal, "button", 0, 0, 1, TRUE, 0, &error)); g_clear_error(&error);
  zen_portal_close(&portal);
  g_atomic_int_set(&mock.wrong_session, FALSE);
  g_atomic_int_set(&mock.hold_start, TRUE);
  GCancellable *cancel = g_cancellable_new();
  zen_portal_init(&portal, client, cancel);
  g_timeout_add(20, cancel_pending, cancel);
  g_assert_false(zen_portal_open(&portal, TRUE, "", &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_CANCELLED); g_clear_error(&error);
  g_assert_cmpint(portal.fd, ==, -1);
  g_assert_cmpint(g_atomic_int_get(&mock.held_starts), ==, 1);
  zen_portal_close(&portal); g_object_unref(cancel);
  g_atomic_int_set(&mock.wrong_session, TRUE);
  zen_portal_init(&portal, client, NULL);
  g_assert_false(zen_portal_open(&portal, TRUE, "", &error));
  g_assert_error(error, G_IO_ERROR, G_IO_ERROR_INVALID_DATA); g_clear_error(&error);
  zen_portal_close(&portal);
  g_dbus_connection_close_sync(client, NULL, NULL); g_object_unref(client);
  g_main_loop_quit(mock.loop); g_thread_join(mock.thread);
  g_main_loop_unref(mock.loop); g_main_context_unref(mock.context);
  g_test_dbus_down(mock.test_bus); g_object_unref(mock.test_bus);
  g_cond_clear(&mock.ready); g_mutex_clear(&mock.mutex);
}

int main(int argc, char **argv) {
  g_test_init(&argc, &argv, NULL);
  gst_init(NULL, NULL);
  GError *error = NULL;
  const char *plugin_path = g_getenv("ZEN_DESKTOP_TEST_PIPEWIRE_PLUGIN");
  g_assert_nonnull(plugin_path);
  GstPlugin *plugin = gst_plugin_load_file(plugin_path, &error);
  g_assert_no_error(error); g_assert_nonnull(plugin); gst_object_unref(plugin);
  g_test_add_func("/portal/grant-bounds", grant_bounds);
  g_test_add_func("/portal/input-contract", input_contract);
  g_test_add_func("/portal/private-bus-lifecycle", lifecycle);
  return g_test_run();
}
