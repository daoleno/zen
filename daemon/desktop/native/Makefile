CC ?= cc
OUT ?= $(ZEN_BUILD_TMPDIR)/zen-desktop-helper
PACKAGES = gtk+-3.0 gstreamer-app-1.0 gstreamer-video-1.0 x11 xtst

.PHONY: all portal-check
all:
	@test -n "$(OUT)" && test "$(OUT)" != /zen-desktop-helper
	$(CC) -std=c11 -O2 -Wall -Wextra -Werror linux.c -o "$(OUT)" $$(pkg-config --cflags --libs $(PACKAGES))

portal-check:
	@test -n "$(ZEN_BUILD_TMPDIR)"
	$(CC) -std=c11 -O1 -g -Wall -Wextra -Werror portal.c portal-test.c -o "$(ZEN_BUILD_TMPDIR)/portal-test" $$(pkg-config --cflags --libs gio-unix-2.0) -lm
	"$(ZEN_BUILD_TMPDIR)/portal-test"
