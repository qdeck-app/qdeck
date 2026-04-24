//go:build darwin && !ios

#include "_cgo_export.h"
#import <AppKit/AppKit.h>
#import <objc/runtime.h>

// Associated object key for storing the Go handle on the view.
static const void *kGoHandleKey = &kGoHandleKey;

// Retrieve the stored Go handle from the view.
static uintptr_t getGoHandle(id self) {
	NSNumber *num = objc_getAssociatedObject(self, kGoHandleKey);
	return (uintptr_t)[num unsignedLongLongValue];
}

// NSDraggingDestination method implementations

static NSDragOperation imp_draggingEntered(id self, SEL _cmd, id<NSDraggingInfo> sender) {
	uintptr_t h = getGoHandle(self);
	if (h != 0) {
		nativeDragEntered(h);
	}
	return NSDragOperationCopy;
}

static NSDragOperation imp_draggingUpdated(id self, SEL _cmd, id<NSDraggingInfo> sender) {
	return NSDragOperationCopy;
}

static void imp_draggingExited(id self, SEL _cmd, id<NSDraggingInfo> sender) {
	uintptr_t h = getGoHandle(self);
	if (h != 0) {
		nativeDragExited(h);
	}
}

static BOOL imp_prepareForDragOperation(id self, SEL _cmd, id<NSDraggingInfo> sender) {
	return YES;
}

static BOOL imp_performDragOperation(id self, SEL _cmd, id<NSDraggingInfo> sender) {
	uintptr_t h = getGoHandle(self);
	if (h == 0) {
		return NO;
	}

	NSPasteboard *pb = [sender draggingPasteboard];
	NSArray<NSURL *> *urls = [pb readObjectsForClasses:@[[NSURL class]]
											   options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
	if (urls == nil || [urls count] == 0) {
		return NO;
	}

	// Collect valid path strings first. The NSMutableArray retains each
	// NSString, keeping their UTF8String buffers alive until after the
	// Go callback copies them. Without this, ARC may release the
	// NSString at the end of the loop body, leaving a dangling pointer.
	int count = (int)[urls count];
	NSMutableArray<NSString *> *pathStrings = [NSMutableArray arrayWithCapacity:count];
	for (int i = 0; i < count; i++) {
		NSString *path = [urls[i] path];
		if (path != nil) {
			[pathStrings addObject:path];
		}
	}

	int valid = (int)[pathStrings count];
	if (valid == 0) {
		return NO;
	}

	const char **cpaths = (const char **)malloc(sizeof(const char *) * valid);
	if (cpaths == NULL) {
		return NO;
	}

	for (int i = 0; i < valid; i++) {
		cpaths[i] = [pathStrings[i] UTF8String];
	}

	// Drop Y in top-left pixel coordinates.
	// AppKit uses bottom-left origin in points; convert to top-left pixels.
	NSPoint loc = [sender draggingLocation];
	CGFloat viewH = [self bounds].size.height;
	CGFloat scale = [[self window] backingScaleFactor];
	float dropY = (float)((viewH - loc.y) * scale);

	nativeDropCallback(h, (char **)cpaths, valid, dropY);
	free(cpaths);

	return YES;
}

// registerDropTarget dynamically adds NSDraggingDestination methods to the
// GioView class and registers it for file URL drags.
void registerDropTarget(CFTypeRef viewRef, uintptr_t handle) {
	NSView *view = (__bridge NSView *)viewRef;

	// Store the Go handle as an associated object on the view instance.
	objc_setAssociatedObject(view,
		kGoHandleKey,
		[NSNumber numberWithUnsignedLongLong:(unsigned long long)handle],
		OBJC_ASSOCIATION_RETAIN_NONATOMIC);

	Class cls = [view class];

	// Add NSDraggingDestination methods (only added once; class_addMethod is a no-op
	// if the selector already exists).
	class_addMethod(cls, @selector(draggingEntered:),
		(IMP)imp_draggingEntered, "l@:@");
	class_addMethod(cls, @selector(draggingUpdated:),
		(IMP)imp_draggingUpdated, "l@:@");
	class_addMethod(cls, @selector(draggingExited:),
		(IMP)imp_draggingExited, "v@:@");
	class_addMethod(cls, @selector(prepareForDragOperation:),
		(IMP)imp_prepareForDragOperation, "c@:@");
	class_addMethod(cls, @selector(performDragOperation:),
		(IMP)imp_performDragOperation, "c@:@");

	// Register the view for file URL drops.
	[view registerForDraggedTypes:@[NSPasteboardTypeFileURL]];
}
