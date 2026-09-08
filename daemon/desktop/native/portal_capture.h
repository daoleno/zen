#ifndef ZEN_DESKTOP_PORTAL_CAPTURE_H
#define ZEN_DESKTOP_PORTAL_CAPTURE_H

#include "portal.h"
#include <gst/gst.h>

/* Bind only a granted portal FD/node; never open the default PipeWire remote. */
gboolean zen_portal_bind_source(ZenPortal *portal, GstElement *source, GError **error);

#endif
