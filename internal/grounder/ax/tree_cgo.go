//go:build darwin && cgo

package ax

/*
#cgo CFLAGS: -x objective-c -Wno-deprecated-declarations
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <stdlib.h>
#include <string.h>

// strAttr reads a string-typed accessibility attribute, returning @"" when it
// is missing or not a string. (MRC — cgo compiles Objective-C without ARC.)
static NSString *strAttr(AXUIElementRef el, CFStringRef attr) {
  CFTypeRef v = NULL;
  if (AXUIElementCopyAttributeValue(el, attr, &v) != 0 || v == NULL) return @"";
  NSString *s = @"";
  if (CFGetTypeID(v) == CFStringGetTypeID()) s = [NSString stringWithString:(NSString *)v];
  CFRelease(v);
  return s;
}

static BOOL boolAttr(AXUIElementRef el, CFStringRef attr) {
  CFTypeRef v = NULL;
  if (AXUIElementCopyAttributeValue(el, attr, &v) != 0 || v == NULL) return NO;
  BOOL b = NO;
  if (CFGetTypeID(v) == CFBooleanGetTypeID()) b = CFBooleanGetValue((CFBooleanRef)v);
  CFRelease(v);
  return b;
}

// frameAttr reads AXPosition/AXSize (each an AXValue wrapping a CGPoint/CGSize)
// into global logical-point coordinates.
static void frameAttr(AXUIElementRef el, double *x, double *y, double *w, double *h) {
  *x = *y = *w = *h = 0;
  CFTypeRef pos = NULL, size = NULL;
  if (AXUIElementCopyAttributeValue(el, kAXPositionAttribute, &pos) == 0 && pos) {
    CGPoint p;
    if (AXValueGetValue((AXValueRef)pos, kAXValueCGPointType, &p)) { *x = p.x; *y = p.y; }
    CFRelease(pos);
  }
  if (AXUIElementCopyAttributeValue(el, kAXSizeAttribute, &size) == 0 && size) {
    CGSize s;
    if (AXValueGetValue((AXValueRef)size, kAXValueCGSizeType, &s)) { *w = s.width; *h = s.height; }
    CFRelease(size);
  }
}

static void walk(AXUIElementRef el, int depth, int maxDepth, int maxElements, NSMutableArray *out) {
  if ((int)out.count >= maxElements || depth > maxDepth) return;
  double x, y, w, h;
  frameAttr(el, &x, &y, &w, &h);
  [out addObject:@{
    @"role": strAttr(el, kAXRoleAttribute),
    @"title": strAttr(el, kAXTitleAttribute),
    @"desc": strAttr(el, kAXDescriptionAttribute),
    @"value": strAttr(el, kAXValueAttribute),
    @"x": @(x), @"y": @(y), @"w": @(w), @"h": @(h),
    @"enabled": @(boolAttr(el, kAXEnabledAttribute))
  }];
  CFTypeRef kids = NULL;
  if (AXUIElementCopyAttributeValue(el, kAXChildrenAttribute, &kids) == 0 && kids) {
    CFArrayRef arr = (CFArrayRef)kids;
    CFIndex n = CFArrayGetCount(arr);
    for (CFIndex i = 0; i < n && (int)out.count < maxElements; i++) {
      walk((AXUIElementRef)CFArrayGetValueAtIndex(arr, i), depth + 1, maxDepth, maxElements, out);
    }
    CFRelease(kids);
  }
}

// argus_ax_walk_json walks the frontmost app's accessibility tree and returns a
// malloc'd JSON string {"screen":{...},"elements":[...]} matching the JXA
// walk's shape, or NULL when the process isn't accessibility-trusted or there
// is no frontmost app. The caller frees the returned buffer.
static char *argus_ax_walk_json(int maxDepth, int maxElements) {
  @autoreleasepool {
    if (!AXIsProcessTrusted()) return NULL;
    NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
    if (app == nil) return NULL;
    AXUIElementRef axapp = AXUIElementCreateApplication(app.processIdentifier);
    if (axapp == NULL) return NULL;
    // Bound every AX IPC call. AXUIElementCopyAttributeValue is a synchronous
    // cross-process call that blocks indefinitely against a frozen/unresponsive
    // target; a per-element timeout makes an unresponsive app fail fast instead
    // of pinning this (cgo) thread forever. Inherited by elements copied from it.
    AXUIElementSetMessagingTimeout(axapp, 2.0f);
    NSMutableArray *elements = [NSMutableArray array];
    walk(axapp, 0, maxDepth, maxElements, elements);
    CFRelease(axapp);
    CGRect mb = CGDisplayBounds(CGMainDisplayID());
    NSDictionary *payload = @{
      @"screen": @{@"w": @(mb.size.width), @"h": @(mb.size.height)},
      @"elements": elements
    };
    NSData *data = [NSJSONSerialization dataWithJSONObject:payload options:0 error:NULL];
    if (data == nil) return NULL;
    char *buf = (char *)malloc(data.length + 1);
    if (buf == NULL) return NULL;
    memcpy(buf, data.bytes, data.length);
    buf[data.length] = '\0';
    return buf;
  }
}

// argus_frontmost_bundle returns a malloc'd copy of the frontmost application's
// bundle identifier (empty string if none / unavailable). The caller frees it.
// Used to verify the app actually in front is the one the caller asked to
// observe before grounding its tree.
static char *argus_frontmost_bundle(void) {
  @autoreleasepool {
    NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
    NSString *bid = app ? app.bundleIdentifier : nil;
    if (bid == nil) bid = @"";
    const char *c = [bid UTF8String];
    size_t n = strlen(c);
    char *buf = (char *)malloc(n + 1);
    if (buf == NULL) return NULL;
    memcpy(buf, c, n + 1);
    return buf;
  }
}
*/
import "C"

import "unsafe"

// nativeWalk walks the frontmost app's accessibility tree via the C
// AXUIElement API — the same family the Clicker uses. Unlike the System Events
// JXA walk it needs no Automation grant and cannot hang, so get_app_state
// works wherever the process has the Accessibility permission. It returns the
// same wire shape parseTree produces, so the scaling/offset/filter is shared.
func nativeWalk() (wireScreen, []wireElement, error) {
	cs := C.argus_ax_walk_json(C.int(maxDepth), C.int(maxElements))
	if cs == nil {
		return wireScreen{}, nil, errNativeUnavailable
	}
	defer C.free(unsafe.Pointer(cs))
	return parseTree([]byte(C.GoString(cs)))
}

// nativeFrontmostBundle returns the frontmost application's bundle identifier,
// or "" if unavailable. Callers use it to confirm the app they intend to
// observe/act on is actually frontmost before grounding its tree.
func nativeFrontmostBundle() string {
	cs := C.argus_frontmost_bundle()
	if cs == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(cs))
	return C.GoString(cs)
}
