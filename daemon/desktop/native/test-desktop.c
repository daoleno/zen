// Owned-display fixture only. No default DISPLAY fallback is permitted.
#include <gtk/gtk.h>
#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>
#include <stdio.h>
#include <string.h>

static unsigned frames, clicks, keys, scrolls;
static char last_key[64] = "none";
static GtkWidget *canvas;

static gboolean draw(GtkWidget *widget, cairo_t *cr, gpointer unused) {
  (void)widget; (void)unused;
  cairo_set_source_rgb(cr, .95, .97, .98); cairo_paint(cr);
  cairo_set_source_rgb(cr, .06, .10, .13);
  cairo_select_font_face(cr, "monospace", CAIRO_FONT_SLANT_NORMAL, CAIRO_FONT_WEIGHT_NORMAL);
  cairo_set_font_size(cr, 30);
  cairo_move_to(cr, 48, 180); cairo_show_text(cr, "ZEN OWNED TEST DESKTOP");
  char text[256];
  snprintf(text, sizeof(text), "Frame %u   Clicks %u   Keys %u   Scroll %u", frames, clicks, keys, scrolls);
  cairo_move_to(cr, 48, 240); cairo_show_text(cr, text);
  snprintf(text, sizeof(text), "Last key: %s", last_key);
  cairo_move_to(cr, 48, 300); cairo_show_text(cr, text);
  cairo_set_source_rgb(cr, .08, .58, .40);
  cairo_rectangle(cr, 48 + (frames * 6) % 1000, 380, 100, 100); cairo_fill(cr);
  cairo_set_source_rgb(cr, .75, .18, .30);
  cairo_rectangle(cr, 48, 540, 200 + clicks * 10, 30); cairo_fill(cr);
  return FALSE;
}

static gboolean tick(gpointer unused) { (void)unused; frames++; gtk_widget_queue_draw(canvas); return G_SOURCE_CONTINUE; }
static gboolean input(GtkWidget *widget, GdkEvent *event, gpointer unused) {
  (void)widget; (void)unused;
  if (event->type == GDK_BUTTON_PRESS) clicks++;
  if (event->type == GDK_KEY_PRESS) {
    keys++;
    snprintf(last_key, sizeof(last_key), "%s", gdk_keyval_name(event->key.keyval));
  }
  if (event->type == GDK_SCROLL) scrolls++;
  if (event->type == GDK_BUTTON_PRESS || event->type == GDK_KEY_PRESS || event->type == GDK_SCROLL) {
    printf("{\"frame\":%u,\"clicks\":%u,\"keys\":%u,\"scrolls\":%u,\"key\":\"%s\"}\n", frames, clicks, keys, scrolls, last_key);
    fflush(stdout);
  }
  return FALSE;
}

int main(int argc, char **argv) {
  if (argc < 3 || strcmp(argv[1], "--display")) return 2;
  g_setenv("DISPLAY", argv[2], TRUE); g_setenv("GDK_BACKEND", "x11", TRUE);
  if (!gtk_init_check(NULL, NULL)) return 3;
  if (argc == 5 && !strcmp(argv[3], "--snapshot")) {
    GdkWindow *root = gdk_get_default_root_window();
    GdkPixbuf *image = gdk_pixbuf_get_from_window(root, 0, 0, gdk_window_get_width(root), gdk_window_get_height(root));
    if (!image) return 4;
    gboolean ok = gdk_pixbuf_save(image, argv[4], "png", NULL, NULL);
    g_object_unref(image); return ok ? 0 : 4;
  }
  if (argc == 4 && (!strcmp(argv[3], "--approve") || !strcmp(argv[3], "--cancel"))) {
    Display *d = XOpenDisplay(argv[2]);
    Window root, parent, *children; unsigned count;
    if (!d || !XQueryTree(d, DefaultRootWindow(d), &root, &parent, &children, &count)) return 4;
    int result = 5;
    for (unsigned i = 0; i < count; i++) {
      char *name = NULL;
      if (XFetchName(d, children[i], &name) && name && !strcmp(name, "Zen Desktop Permission")) {
        XWindowAttributes attrs; XGetWindowAttributes(d, children[i], &attrs);
        XTestFakeMotionEvent(d, DefaultScreen(d), attrs.x + attrs.width - (!strcmp(argv[3], "--approve") ? 70 : 180), attrs.y + attrs.height - 20, CurrentTime);
        XTestFakeButtonEvent(d, 1, True, CurrentTime); XTestFakeButtonEvent(d, 1, False, CurrentTime);
        XFlush(d); result = 0;
      }
      if (name) XFree(name);
    }
    XFree(children); XCloseDisplay(d); return result;
  }
  GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  gtk_window_set_title(GTK_WINDOW(window), "Zen Owned Test Desktop");
  gtk_window_set_default_size(GTK_WINDOW(window), 1280, 720);
  gtk_window_move(GTK_WINDOW(window), 0, 0);
  canvas = gtk_drawing_area_new();
  gtk_widget_set_can_focus(canvas, TRUE);
  gtk_widget_add_events(canvas, GDK_BUTTON_PRESS_MASK | GDK_KEY_PRESS_MASK | GDK_SCROLL_MASK);
  gtk_container_add(GTK_CONTAINER(window), canvas);
  g_signal_connect(canvas, "draw", G_CALLBACK(draw), NULL);
  g_signal_connect(canvas, "event", G_CALLBACK(input), NULL);
  g_signal_connect(window, "destroy", G_CALLBACK(gtk_main_quit), NULL);
  gtk_widget_show_all(window); gtk_widget_grab_focus(canvas);
  g_timeout_add(33, tick, NULL); gtk_main(); return 0;
}
