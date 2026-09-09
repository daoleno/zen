CC ?= cc
OUT ?= $(ZEN_BUILD_TMPDIR)/zen-desktop-helper
PACKAGES = gtk+-3.0 gstreamer-app-1.0 gstreamer-video-1.0 x11 xtst gio-unix-2.0

.PHONY: all portal-check encoder-check
all:
	@test -n "$(OUT)" && test "$(OUT)" != /zen-desktop-helper
	$(CC) -std=c11 -O2 -Wall -Wextra -Werror linux.c portal.c portal_capture.c encoder.c -o "$(OUT)" $$(pkg-config --cflags --libs $(PACKAGES))

encoder-check:
	@test -n "$(ZEN_BUILD_TMPDIR)"
	$(CC) -std=c11 -O1 -g -Wall -Wextra -Werror encoder.c encoder-test.c -o "$(ZEN_BUILD_TMPDIR)/encoder-test" $$(pkg-config --cflags --libs gio-unix-2.0 gstreamer-app-1.0 gstreamer-video-1.0)
	GST_PLUGIN_SYSTEM_PATH_1_0= GST_PLUGIN_PATH_1_0= ZEN_ENCODER_TEST_PLUGINS="$$(pkg-config --variable=pluginsdir gstreamer-1.0)" "$(ZEN_BUILD_TMPDIR)/encoder-test"

portal-check:
	@test -n "$(ZEN_BUILD_TMPDIR)"
	$(CC) -std=c11 -O1 -g -Wall -Wextra -Werror portal.c portal_capture.c portal-test.c -o "$(ZEN_BUILD_TMPDIR)/portal-test" $$(pkg-config --cflags --libs gio-unix-2.0 gstreamer-1.0) -lm
	GST_REGISTRY="$(ZEN_BUILD_TMPDIR)/portal-test-registry.bin" GST_REGISTRY_UPDATE=no GST_PLUGIN_SYSTEM_PATH_1_0= GST_PLUGIN_PATH_1_0= ZEN_DESKTOP_TEST_PIPEWIRE_PLUGIN="$$(pkg-config --variable=pluginsdir gstreamer-1.0)/libgstpipewire.so" "$(ZEN_BUILD_TMPDIR)/portal-test"
