#define _DEFAULT_SOURCE
#include <X11/Xlib.h>
#include <X11/Xatom.h>
#include <X11/keysym.h>
#include <X11/extensions/XTest.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int window_name(Display *display, Window window, char *out, size_t size) {
  Atom net_name = XInternAtom(display, "_NET_WM_NAME", False);
  Atom actual;
  int format;
  unsigned long count, bytes;
  unsigned char *data = NULL;
  if (XGetWindowProperty(display, window, net_name, 0, 256, False, AnyPropertyType,
        &actual, &format, &count, &bytes, &data) == Success && data) {
    snprintf(out, size, "%s", (char *)data);
    XFree(data);
    return 1;
  }
  char *legacy = NULL;
  if (XFetchName(display, window, &legacy) && legacy) {
    snprintf(out, size, "%s", legacy);
    XFree(legacy);
    return 1;
  }
  return 0;
}

static Window find_named(Display *display, Window window, const char *want) {
  char name[256];
  if (window_name(display, window, name, sizeof(name)) && strstr(name, want))
    return window;
  Window root, parent, *children = NULL;
  unsigned n = 0;
  if (!XQueryTree(display, window, &root, &parent, &children, &n) || !children)
    return 0;
  Window found = 0;
  for (unsigned i = 0; i < n && !found; i++)
    found = find_named(display, children[i], want);
  XFree(children);
  return found;
}

int main(void) {
  Display *display = XOpenDisplay(NULL);
  if (!display) {
    fprintf(stderr, "test-grant-click: no display\n");
    return 2;
  }
  int event, error, major, minor;
  if (!XTestQueryExtension(display, &event, &error, &major, &minor)) {
    fprintf(stderr, "test-grant-click: no XTest\n");
    XCloseDisplay(display);
    return 2;
  }
  Window target = find_named(display, DefaultRootWindow(display), "Zen Desktop Permission");
  if (!target)
    target = find_named(display, DefaultRootWindow(display), "Zen Desktop");
  if (target) {
    XMapRaised(display, target);
    XSetInputFocus(display, target, RevertToParent, CurrentTime);
    XWindowAttributes attrs;
    if (XGetWindowAttributes(display, target, &attrs) && attrs.width > 8 && attrs.height > 8) {
      int x = attrs.x + (attrs.width * 4) / 5;
      int y = attrs.y + (attrs.height * 4) / 5;
      XTestFakeMotionEvent(display, DefaultScreen(display), x, y, CurrentTime);
      XTestFakeButtonEvent(display, 1, True, CurrentTime);
      XTestFakeButtonEvent(display, 1, False, CurrentTime);
      XFlush(display);
      usleep(50000);
    }
  }
  KeyCode tab = XKeysymToKeycode(display, XK_Tab);
  KeyCode enter = XKeysymToKeycode(display, XK_Return);
  if (tab && enter) {
    XTestFakeKeyEvent(display, tab, True, CurrentTime);
    XTestFakeKeyEvent(display, tab, False, CurrentTime);
    XFlush(display);
    usleep(30000);
    XTestFakeKeyEvent(display, enter, True, CurrentTime);
    XTestFakeKeyEvent(display, enter, False, CurrentTime);
    XFlush(display);
  }
  XCloseDisplay(display);
  return target ? 0 : 1;
}
