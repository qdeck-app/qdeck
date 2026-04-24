//go:build darwin && !ios

#include "_cgo_export.h"
#import <AppKit/AppKit.h>
#import <objc/runtime.h>

// Window close guard

// Global flag: when YES, windowShouldClose: returns NO and notifies Go instead.
static BOOL gCloseGuarded = NO;
// Stash the Go handle so the close callback can reach it.
static uintptr_t gCloseHandle = 0;
// Original IMP for windowShouldClose: (may be nil if the delegate had none).
static IMP gOrigWindowShouldClose = NULL;
// Whether swizzling has already been applied.
static BOOL gCloseSwizzled = NO;

static BOOL imp_windowShouldClose(id self, SEL _cmd, id sender) {
	if (gCloseGuarded && gCloseHandle != 0) {
		closeGuardAttempted(gCloseHandle);
		return NO;
	}
	if (gOrigWindowShouldClose != NULL) {
		return ((BOOL(*)(id, SEL, id))gOrigWindowShouldClose)(self, _cmd, sender);
	}
	return YES;
}

void setCloseGuard(_Bool guarded) {
	gCloseGuarded = guarded;
}

void installCloseGuard(CFTypeRef viewRef, uintptr_t handle) {
	gCloseHandle = handle;
	if (gCloseSwizzled) {
		return;
	}
	// Must run on the main thread because we access NSWindow's delegate
	// and swizzle methods on it. The caller (ListenEvents) runs from the
	// main Gio event loop, which is the AppKit main thread — but we use
	// dispatch_async to ensure safety if the call order ever changes.
	NSView *capturedView = (__bridge NSView *)viewRef;
	dispatch_async(dispatch_get_main_queue(), ^{
		NSView *view = capturedView;
		if (gCloseSwizzled) {
			return;
		}
		NSWindow *window = [view window];
		if (window == nil) {
			return;
		}
		id delegate = [window delegate];
		if (delegate == nil) {
			return;
		}
		Class cls = [delegate class];
		SEL sel = @selector(windowShouldClose:);
		Method m = class_getInstanceMethod(cls, sel);
		if (m != NULL) {
			gOrigWindowShouldClose = method_getImplementation(m);
			method_setImplementation(m, (IMP)imp_windowShouldClose);
		} else {
			class_addMethod(cls, sel, (IMP)imp_windowShouldClose, "c@:@");
		}
		gCloseSwizzled = YES;
	});
}
