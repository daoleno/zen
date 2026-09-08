// System-library helper. Never capture before the visible local grant.
#include <gtk/gtk.h>
#include <glib-unix.h>
#include <gst/app/gstappsink.h>
#include <gst/video/video.h>
#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>
#include <arpa/inet.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static Display *display;
static GstElement *pipeline;
static GtkWidget *window;
static gboolean control, granted;
static gboolean keys[256], buttons[4];
static int source_width, source_height;
static const char *display_name;

static gboolean source_changed(gpointer unused) {
  (void)unused;
  XWindowAttributes attrs;
  if (!XGetWindowAttributes(display, DefaultRootWindow(display), &attrs) ||
      attrs.width != source_width || attrs.height != source_height) {
    gtk_main_quit();
    return G_SOURCE_REMOVE;
  }
  return G_SOURCE_CONTINUE;
}

static gboolean packet(unsigned char type, const void *data, size_t size) {
  if (size > (4u << 20) - 1) return FALSE;
  uint32_t length = htonl((uint32_t)size + 1);
  return fwrite(&length, 4, 1, stdout) == 1 &&
    fwrite(&type, 1, 1, stdout) == 1 &&
    fwrite(data, 1, size, stdout) == size && fflush(stdout) == 0;
}

static void release_input(void) {
  if (!display) return;
  for (int i = 1; i < 256; i++) {
    if (keys[i]) XTestFakeKeyEvent(display, i, False, CurrentTime);
    keys[i] = FALSE;
  }
  for (int i = 1; i < 4; i++) {
    if (buttons[i]) XTestFakeButtonEvent(display, i, False, CurrentTime);
    buttons[i] = FALSE;
  }
  XFlush(display);
}

static gboolean stop_idle(gpointer unused) {
  (void)unused;
  gtk_main_quit();
  return G_SOURCE_REMOVE;
}

static GstFlowReturn sample(GstAppSink *sink, gpointer unused) {
  (void)unused;
  GstSample *s = gst_app_sink_pull_sample(sink);
  if (!s) return GST_FLOW_EOS;
  GstMapInfo map;
  GstBuffer *buffer = gst_sample_get_buffer(s);
  gboolean ok = gst_buffer_map(buffer, &map, GST_MAP_READ);
  if (ok) {
    ok = packet(2, map.data, map.size);
    gst_buffer_unmap(buffer, &map);
  }
  gst_sample_unref(s);
  if (!ok) g_idle_add(stop_idle, NULL);
  return ok ? GST_FLOW_OK : GST_FLOW_ERROR;
}

static gboolean bus_message(GstBus *bus, GstMessage *message, gpointer unused) {
  (void)bus; (void)unused;
  if (GST_MESSAGE_TYPE(message) == GST_MESSAGE_ERROR || GST_MESSAGE_TYPE(message) == GST_MESSAGE_EOS)
    gtk_main_quit();
  return G_SOURCE_CONTINUE;
}

static gboolean read_input(GIOChannel *channel, GIOCondition condition, gpointer unused) {
  (void)unused;
  if (condition & (G_IO_HUP | G_IO_ERR)) { gtk_main_quit(); return G_SOURCE_REMOVE; }
  gchar *line = NULL;
  gsize length = 0;
  GIOStatus result = g_io_channel_read_line(channel, &line, &length, NULL, NULL);
  if (result == G_IO_STATUS_EOF || result == G_IO_STATUS_ERROR || length > 256) {
    g_free(line); gtk_main_quit(); return G_SOURCE_REMOVE;
  }
  if (line && granted && control) {
    char type[16], down[8]; double x, y; unsigned code; int delta;
    if (sscanf(line, "%15s %lf %lf %u %7s %d", type, &x, &y, &code, down, &delta) == 6) {
      gboolean pressed = strcmp(down, "true") == 0;
      if (!strcmp(type, "release")) release_input();
      else if (!strcmp(type, "pointer") && x >= 0 && x <= 1 && y >= 0 && y <= 1)
        XTestFakeMotionEvent(display, DefaultScreen(display), (int)(x * (source_width - 1)), (int)(y * (source_height - 1)), CurrentTime);
      else if (!strcmp(type, "button") && code >= 1 && code <= 3) {
        buttons[code] = pressed;
        XTestFakeButtonEvent(display, code, pressed, CurrentTime);
      } else if (!strcmp(type, "key")) {
        KeyCode key = XKeysymToKeycode(display, code);
        if (key) { keys[key] = pressed; XTestFakeKeyEvent(display, key, pressed, CurrentTime); }
      } else if (!strcmp(type, "scroll") && delta >= -10 && delta <= 10) {
        for (int i = 0; i < abs(delta); i++) {
          unsigned button = delta > 0 ? 5 : 4;
          XTestFakeButtonEvent(display, button, True, CurrentTime);
          XTestFakeButtonEvent(display, button, False, CurrentTime);
        }
      }
      XFlush(display);
    }
  }
  g_free(line);
  return G_SOURCE_CONTINUE;
}

static void stop_clicked(GtkWidget *widget, gpointer unused) {
  (void)widget; (void)unused;
  gtk_main_quit();
}

static void approve(GtkDialog *dialog, gint response, gpointer unused) {
  (void)unused;
  if (response != GTK_RESPONSE_ACCEPT) {
    const char *denied = "{\"state\":\"denied\",\"reason\":\"Desktop sharing was declined.\"}";
    packet(1, denied, strlen(denied));
    gtk_main_quit(); return;
  }
  gtk_widget_hide(GTK_WIDGET(dialog));
  int width = MIN(source_width, 1280);
  int height = (int)((double)source_height * width / source_width);
  if (height > 720) { width = (int)((double)width * 720 / height); height = 720; }
  width &= ~1; height &= ~1;
  GError *error = NULL;
  // Only constant element names; display selection is a typed GObject property.
  char *description = g_strdup_printf(
    "ximagesrc name=capture use-damage=false show-pointer=true ! "
    "video/x-raw,framerate=30/1 ! queue max-size-buffers=2 max-size-bytes=0 max-size-time=0 leaky=downstream ! "
    "videoconvert ! videoscale ! video/x-raw,format=I420,width=%d,height=%d ! "
    "openh264enc usage-type=screen complexity=low bitrate=4000000 max-bitrate=4000000 "
    "rate-control=bitrate gop-size=30 multi-thread=2 ! h264parse config-interval=-1 ! "
    "video/x-h264,stream-format=byte-stream,alignment=au,profile=constrained-baseline ! "
    "appsink name=output emit-signals=true sync=false max-buffers=2 drop=false", width, height);
  pipeline = gst_parse_launch(description, &error);
  g_free(description);
  if (error || !pipeline) {
    const char *failure = "{\"state\":\"unsupported\",\"reason\":\"Required GStreamer capture or H.264 plugins are unavailable.\"}";
    packet(1, failure, strlen(failure));
    g_clear_error(&error); gtk_main_quit(); return;
  }
  GstElement *capture = gst_bin_get_by_name(GST_BIN(pipeline), "capture");
  g_object_set(capture, "display-name", display_name, NULL);
  gst_object_unref(capture);
  GstElement *output = gst_bin_get_by_name(GST_BIN(pipeline), "output");
  g_signal_connect(output, "new-sample", G_CALLBACK(sample), NULL);
  gst_object_unref(output);
  GstBus *bus = gst_element_get_bus(pipeline);
  gst_bus_add_watch(bus, bus_message, NULL);
  gst_object_unref(bus);
  char metadata[256];
  snprintf(metadata, sizeof(metadata), "{\"state\":\"streaming\",\"width\":%d,\"height\":%d,\"codec\":\"h264\",\"encoder\":\"openh264-software\",\"control\":%s}", width, height, control ? "true" : "false");
  packet(1, metadata, strlen(metadata));
  window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  gtk_window_set_title(GTK_WINDOW(window), "Zen Desktop Sharing");
  gtk_window_set_keep_above(GTK_WINDOW(window), TRUE);
  GtkWidget *stop = gtk_button_new_with_label(control ? "Stop desktop control" : "Stop desktop sharing");
  gtk_container_add(GTK_CONTAINER(window), stop);
  g_signal_connect(window, "destroy", G_CALLBACK(stop_clicked), NULL);
  g_signal_connect(stop, "clicked", G_CALLBACK(stop_clicked), NULL);
  gtk_widget_show_all(window);
  granted = TRUE;
  if (gst_element_set_state(pipeline, GST_STATE_PLAYING) == GST_STATE_CHANGE_FAILURE) gtk_main_quit();
}

int main(int argc, char **argv) {
  const char *device = NULL;
  for (int i = 1; i < argc; i++) {
    if (!strcmp(argv[i], "--display") && i + 1 < argc) display_name = argv[++i];
    else if (!strcmp(argv[i], "--device") && i + 1 < argc) device = argv[++i];
    else if (!strcmp(argv[i], "--control")) control = TRUE;
    else return 2;
  }
  if (!display_name || !device || strlen(device) > 256) return 2;
  // Never inherit a different personal desktop or select Wayland implicitly.
  g_setenv("DISPLAY", display_name, TRUE);
  g_setenv("GDK_BACKEND", "x11", TRUE);
  XInitThreads();
  signal(SIGPIPE, SIG_IGN);
  gst_init(NULL, NULL);
  if (!gtk_init_check(NULL, NULL)) return 3;
  display = XOpenDisplay(display_name);
  if (!display) return 3;
  int event, error, major, minor;
  if (control && !XTestQueryExtension(display, &event, &error, &major, &minor)) return 3;
  source_width = DisplayWidth(display, DefaultScreen(display));
  source_height = DisplayHeight(display, DefaultScreen(display));
  g_timeout_add(1000, source_changed, NULL);
  g_unix_signal_add(SIGTERM, stop_idle, NULL);
  g_unix_signal_add(SIGINT, stop_idle, NULL);
  GIOChannel *input = g_io_channel_unix_new(STDIN_FILENO);
  g_io_channel_set_flags(input, G_IO_FLAG_NONBLOCK, NULL);
  g_io_add_watch(input, G_IO_IN | G_IO_HUP | G_IO_ERR, read_input, NULL);
  GtkWidget *dialog = gtk_message_dialog_new(NULL, GTK_DIALOG_MODAL,
    GTK_MESSAGE_QUESTION, GTK_BUTTONS_NONE, "%s requests %s", device, control ? "desktop control" : "desktop viewing");
  gtk_window_set_title(GTK_WINDOW(dialog), "Zen Desktop Permission");
  gtk_message_dialog_format_secondary_text(GTK_MESSAGE_DIALOG(dialog), "X11 desktop %s (%d x %d)", display_name, source_width, source_height);
  gtk_dialog_add_buttons(GTK_DIALOG(dialog), "Cancel", GTK_RESPONSE_CANCEL,
    control ? "Allow control" : "Allow viewing", GTK_RESPONSE_ACCEPT, NULL);
  gtk_dialog_set_default_response(GTK_DIALOG(dialog), GTK_RESPONSE_CANCEL);
  g_signal_connect(dialog, "response", G_CALLBACK(approve), NULL);
  const char *requesting = "{\"state\":\"requesting\"}";
  packet(1, requesting, strlen(requesting));
  gtk_widget_show_all(dialog);
  gtk_main();
  release_input();
  if (pipeline) { gst_element_set_state(pipeline, GST_STATE_NULL); gst_object_unref(pipeline); }
  g_io_channel_unref(input);
  XCloseDisplay(display);
  return 0;
}
