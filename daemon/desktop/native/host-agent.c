#define _GNU_SOURCE
#include <X11/Xlib.h>
#include <X11/XKBlib.h>
#include <X11/keysym.h>
#include <X11/extensions/XTest.h>
#include <arpa/inet.h>
#include <glib-unix.h>
#include <gst/app/gstappsink.h>
#include <math.h>
#include <linux/capability.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <unistd.h>

// This executable is only an inherited root-broker capability consumer. It is
// not an unattended flag on the consent-based user helper and never runs as root.
static Display *display;
static GMainLoop *loop;
static GstElement *pipeline;
static gboolean held[256], buttons[4], control;
static int width, height;
static const int channel = 3;

static void release_input(void) {
  for (int i = 1; i < 256; i++) {
    if (held[i]) XTestFakeKeyEvent(display, i, False, CurrentTime);
    held[i] = FALSE;
  }
  for (int i = 1; i < 4; i++) {
    if (buttons[i]) XTestFakeButtonEvent(display, i, False, CurrentTime);
    buttons[i] = FALSE;
  }
  XSync(display, False);
}

static gboolean stop(gpointer unused) {
  (void)unused;
  g_main_loop_quit(loop);
  return G_SOURCE_REMOVE;
}

static gboolean write_all(const void *bytes, size_t length) {
  const unsigned char *p = bytes;
  while (length) {
    ssize_t n = send(channel, p, length, MSG_NOSIGNAL);
    if (n <= 0) return FALSE;
    p += n;
    length -= (size_t)n;
  }
  return TRUE;
}

static gboolean packet(unsigned char kind, const void *data, size_t size) {
  if (size == 0 || size > (4u << 20) - 1) return FALSE;
  uint32_t length = htonl((uint32_t)size + 1);
  return write_all(&length, sizeof(length)) && write_all(&kind, 1) && write_all(data, size);
}

static GstFlowReturn frame(GstAppSink *sink, gpointer unused) {
  (void)unused;
  GstSample *sample = gst_app_sink_pull_sample(sink);
  if (!sample) return GST_FLOW_EOS;
  GstBuffer *buffer = gst_sample_get_buffer(sample);
  GstMapInfo map;
  gboolean ok = gst_buffer_map(buffer, &map, GST_MAP_READ);
  if (ok) {
    ok = packet(2, map.data, map.size);
    gst_buffer_unmap(buffer, &map);
  }
  gst_sample_unref(sample);
  if (!ok) g_idle_add(stop, NULL);
  return ok ? GST_FLOW_OK : GST_FLOW_ERROR;
}

static gboolean bus_message(GstBus *bus, GstMessage *message, gpointer unused) {
  (void)bus; (void)unused;
  if (GST_MESSAGE_TYPE(message) == GST_MESSAGE_ERROR || GST_MESSAGE_TYPE(message) == GST_MESSAGE_EOS)
    stop(NULL);
  return G_SOURCE_CONTINUE;
}

// Text is an atomic character event, separate from held shortcut keys. Refuse
// unavailable keyboard symbols instead of modifying the system keyboard map.
static gboolean text_key(unsigned code) {
  if (code < 0x20 || code > 0x7e) return FALSE;
  KeyCode key = XKeysymToKeycode(display, code);
  if (!key) return FALSE;
  int level = XkbKeycodeToKeysym(display, key, 0, 0) == code ? 0 :
    XkbKeycodeToKeysym(display, key, 0, 1) == code ? 1 : -1;
  if (level < 0) return FALSE;
  release_input();
  KeyCode shift = XKeysymToKeycode(display, XK_Shift_L);
  if (level) XTestFakeKeyEvent(display, shift, True, CurrentTime);
  XTestFakeKeyEvent(display, key, True, CurrentTime);
  XTestFakeKeyEvent(display, key, False, CurrentTime);
  if (level) XTestFakeKeyEvent(display, shift, False, CurrentTime);
  XSync(display, False);
  return TRUE;
}

static gboolean command(const char *line) {
  char kind[16], down[8], extra;
  double x, y;
  unsigned code;
  int delta;
  if (sscanf(line, "%15s %lf %lf %u %7s %d %c", kind, &x, &y, &code, down, &delta, &extra) != 6)
    return FALSE;
  gboolean pressed = strcmp(down, "true") == 0;
  if (strcmp(down, "false") && !pressed) return FALSE;
  if (!strcmp(kind, "release")) { release_input(); return TRUE; }
  if (!control) return FALSE;
  if (!strcmp(kind, "pointer") && isfinite(x) && isfinite(y) && x >= 0 && x <= 1 && y >= 0 && y <= 1)
    XTestFakeMotionEvent(display, DefaultScreen(display), (int)(x * (width - 1)), (int)(y * (height - 1)), CurrentTime);
  else if (!strcmp(kind, "button") && code >= 1 && code <= 3) {
    buttons[code] = pressed;
    XTestFakeButtonEvent(display, code, pressed, CurrentTime);
  } else if (!strcmp(kind, "key") && ((code >= 0x20 && code <= 0x7e) || (code >= 0xff08 && code <= 0xffff))) {
    KeyCode key = XKeysymToKeycode(display, code);
    if (!key) return FALSE;
    held[key] = pressed;
    XTestFakeKeyEvent(display, key, pressed, CurrentTime);
  } else if (!strcmp(kind, "text")) return text_key(code);
  else if (!strcmp(kind, "scroll") && delta >= -10 && delta <= 10) {
    for (int i = 0; i < abs(delta); i++) {
      unsigned button = delta > 0 ? 5 : 4;
      XTestFakeButtonEvent(display, button, True, CurrentTime);
      XTestFakeButtonEvent(display, button, False, CurrentTime);
    }
  } else return FALSE;
  XSync(display, False);
  return TRUE;
}

static gboolean input(gint fd, GIOCondition condition, gpointer unused) {
  (void)unused;
  static char line[256];
  static size_t used;
  if (condition & (G_IO_HUP | G_IO_ERR)) { stop(NULL); return G_SOURCE_REMOVE; }
  unsigned char data[1024];
  ssize_t n = recv(fd, data, sizeof(data), MSG_DONTWAIT);
  if (n <= 0) { stop(NULL); return G_SOURCE_REMOVE; }
  for (ssize_t i = 0; i < n; i++) {
    if (data[i] == '\n') {
      line[used] = 0;
      gboolean ok = command(line);
      memset(line, 0, sizeof(line)); used = 0;
      if (!ok) { stop(NULL); return G_SOURCE_REMOVE; }
    } else {
      if (data[i] == 0 || used >= sizeof(line) - 1) { stop(NULL); return G_SOURCE_REMOVE; }
      line[used++] = (char)data[i];
    }
  }
  memset(data, 0, sizeof(data));
  return G_SOURCE_CONTINUE;
}

static gboolean geometry(gpointer unused) {
  (void)unused;
  XWindowAttributes attrs;
  if (!XGetWindowAttributes(display, DefaultRootWindow(display), &attrs) || attrs.width != width || attrs.height != height)
    return stop(NULL);
  return G_SOURCE_CONTINUE;
}

int zen_desktop_agent_main(int argc, char **argv) {
  struct ucred peer;
  socklen_t peer_size = sizeof(peer);
  struct stat authority;
  int argi = 1;
  if (argi < argc && !strcmp(argv[argi], "desktop-agent")) argi++;
  if (argc - argi != 2 || getuid() == 0 || geteuid() != getuid() ||
      getsockopt(channel, SOL_SOCKET, SO_PEERCRED, &peer, &peer_size) || peer.uid != 0 || peer.pid != getppid() ||
      fstat(4, &authority) || !S_ISREG(authority.st_mode) || authority.st_size < 1 || authority.st_size > 65536)
    return 2;
  const char *display_arg = argv[argi];
  const char *mode_arg = argv[argi + 1];
  if (display_arg[0] != ':' || strlen(display_arg) > 16 || strspn(display_arg + 1, "0123456789.") != strlen(display_arg + 1)) return 2;
  if (strcmp(mode_arg, "view") && strcmp(mode_arg, "control")) return 2;
  control = !strcmp(mode_arg, "control");
  struct __user_cap_header_struct cap_header = { .version = _LINUX_CAPABILITY_VERSION_3 };
  struct __user_cap_data_struct caps[2] = {{0}, {0}};
  if (prctl(PR_SET_DUMPABLE, 0) || prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) || syscall(SYS_capset, &cap_header, caps)) return 2;
  struct timeval timeout = { .tv_sec = 2 };
  setsockopt(channel, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout));
  signal(SIGPIPE, SIG_IGN);
  setenv("DISPLAY", display_arg, 1);
  setenv("XAUTHORITY", "/proc/self/fd/4", 1);
  XInitThreads();
  display = XOpenDisplay(display_arg);
  if (!display) return 3;
  int event, error, major, minor;
  if (!XTestQueryExtension(display, &event, &error, &major, &minor)) return 3;
  width = DisplayWidth(display, DefaultScreen(display)); height = DisplayHeight(display, DefaultScreen(display));
  if (width < 2 || height < 2 || width > 4096 || height > 4096) return 3;
  int out_w = MIN(width, 1280), out_h = height * out_w / width;
  if (out_h > 720) { out_w = out_w * 720 / out_h; out_h = 720; }
  out_w &= ~1; out_h &= ~1;
  gst_init(NULL, NULL);
  char spec[1024];
  snprintf(spec, sizeof(spec), "ximagesrc use-damage=false show-pointer=true ! video/x-raw,framerate=30/1 ! queue max-size-buffers=2 max-size-bytes=0 max-size-time=0 leaky=downstream ! videoscale ! video/x-raw,width=%d,height=%d ! videoconvert ! video/x-raw,format=I420 ! openh264enc bitrate=4000000 gop-size=30 ! h264parse config-interval=-1 ! video/x-h264,stream-format=byte-stream,alignment=au ! appsink name=output emit-signals=true sync=false max-buffers=2 drop=false", out_w, out_h);
  GError *failure = NULL;
  pipeline = gst_parse_launch(spec, &failure);
  if (!pipeline || failure) { g_clear_error(&failure); return 3; }
  GstElement *sink = gst_bin_get_by_name(GST_BIN(pipeline), "output");
  if (!sink) return 3;
  loop = g_main_loop_new(NULL, FALSE);
  g_signal_connect(sink, "new-sample", G_CALLBACK(frame), NULL);
  gst_object_unref(sink);
  GstBus *bus = gst_element_get_bus(pipeline);
  gst_bus_add_watch(bus, bus_message, NULL); gst_object_unref(bus);
  g_unix_fd_add(channel, G_IO_IN | G_IO_HUP | G_IO_ERR, input, NULL);
  g_unix_signal_add(SIGTERM, stop, NULL); g_unix_signal_add(SIGINT, stop, NULL);
  g_timeout_add(100, geometry, NULL);
  char metadata[256];
  snprintf(metadata, sizeof(metadata), "{\"state\":\"streaming\",\"codec\":\"h264\",\"width\":%d,\"height\":%d,\"control\":%s,\"sensitiveInput\":%s}", out_w, out_h, control ? "true" : "false", control ? "true" : "false");
  if (!packet(1, metadata, strlen(metadata))) return 3;
  if (gst_element_set_state(pipeline, GST_STATE_PLAYING) == GST_STATE_CHANGE_FAILURE) return 3;
  g_main_loop_run(loop);
  release_input();
  gst_element_set_state(pipeline, GST_STATE_NULL); gst_object_unref(pipeline);
  XCloseDisplay(display); g_main_loop_unref(loop);
  return 0;
}

#ifndef ZEN_DESKTOP_CGO
int main(int argc, char **argv) {
  return zen_desktop_agent_main(argc, argv);
}
#endif
