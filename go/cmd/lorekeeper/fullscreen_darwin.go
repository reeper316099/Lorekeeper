//go:build desktop && darwin

package main

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Wails v2 only gives its window the "can enter full screen" flag when the app *starts* in full
// screen, so the green button just zooms and Toggle Full Screen does nothing. Set the flag ourselves.
static void enableFullscreen(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		for (NSWindow *w in [NSApp windows]) {
			[w setCollectionBehavior:[w collectionBehavior] | NSWindowCollectionBehaviorFullScreenPrimary];
		}
	});
}

static int fullscreenCapable(void) {
	__block int ok = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{
		for (NSWindow *w in [NSApp windows]) {
			if (([w collectionBehavior] & NSWindowCollectionBehaviorFullScreenPrimary) &&
			    [[w standardWindowButton:NSWindowZoomButton] isEnabled]) ok = 1;
		}
	});
	return ok;
}
*/
import "C"

func enableFullscreen()       { C.enableFullscreen() }
func fullscreenCapable() bool { return C.fullscreenCapable() == 1 }
