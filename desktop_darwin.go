package main

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa -framework WebKit

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

@interface ManagerWindowDelegate : NSObject <NSWindowDelegate, WKUIDelegate, NSApplicationDelegate>
@end

@implementation ManagerWindowDelegate
- (void)windowWillClose:(NSNotification *)notification {
	[NSApp stop:nil];
}

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
	[sender stop:nil];
	return NSTerminateCancel;
}

- (void)webView:(WKWebView *)webView
	runJavaScriptAlertPanelWithMessage:(NSString *)message
	initiatedByFrame:(WKFrameInfo *)frame
	completionHandler:(void (^)(void))completionHandler {
	NSAlert *alert = [[NSAlert alloc] init];
	[alert setMessageText:message ?: @""];
	[alert addButtonWithTitle:@"OK"];
	NSWindow *window = [webView window];
	if (window == nil) {
		[alert runModal];
		completionHandler();
		return;
	}
	[alert beginSheetModalForWindow:window completionHandler:^(NSModalResponse response) {
		completionHandler();
	}];
}

- (void)webView:(WKWebView *)webView
	runJavaScriptConfirmPanelWithMessage:(NSString *)message
	initiatedByFrame:(WKFrameInfo *)frame
	completionHandler:(void (^)(BOOL result))completionHandler {
	NSAlert *alert = [[NSAlert alloc] init];
	[alert setMessageText:message ?: @""];
	[alert addButtonWithTitle:@"OK"];
	[alert addButtonWithTitle:@"Cancel"];
	NSWindow *window = [webView window];
	if (window == nil) {
		completionHandler([alert runModal] == NSAlertFirstButtonReturn);
		return;
	}
	[alert beginSheetModalForWindow:window completionHandler:^(NSModalResponse response) {
		completionHandler(response == NSAlertFirstButtonReturn);
	}];
}
@end

static ManagerWindowDelegate *managerWindowDelegate;

static NSMenuItem *addMenuItem(NSMenu *menu, NSString *title, SEL action, NSString *keyEquivalent, NSEventModifierFlags modifiers) {
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:keyEquivalent];
	[item setKeyEquivalentModifierMask:modifiers];
	[menu addItem:item];
	return item;
}

static void installManagerMenu(NSString *appTitle) {
	NSMenu *mainMenu = [[NSMenu alloc] initWithTitle:@""];

	NSMenuItem *appMenuItem = [[NSMenuItem alloc] initWithTitle:@"" action:nil keyEquivalent:@""];
	[mainMenu addItem:appMenuItem];
	NSMenu *appMenu = [[NSMenu alloc] initWithTitle:appTitle];
	[appMenu addItemWithTitle:[NSString stringWithFormat:@"Quit %@", appTitle]
		action:@selector(terminate:)
		keyEquivalent:@"q"];
	[appMenuItem setSubmenu:appMenu];

	NSMenuItem *editMenuItem = [[NSMenuItem alloc] initWithTitle:@"" action:nil keyEquivalent:@""];
	[mainMenu addItem:editMenuItem];
	NSMenu *editMenu = [[NSMenu alloc] initWithTitle:@"Edit"];
	addMenuItem(editMenu, @"Undo", @selector(undo:), @"z", NSEventModifierFlagCommand);
	addMenuItem(editMenu, @"Redo", @selector(redo:), @"z", NSEventModifierFlagCommand | NSEventModifierFlagShift);
	[editMenu addItem:[NSMenuItem separatorItem]];
	addMenuItem(editMenu, @"Cut", @selector(cut:), @"x", NSEventModifierFlagCommand);
	addMenuItem(editMenu, @"Copy", @selector(copy:), @"c", NSEventModifierFlagCommand);
	addMenuItem(editMenu, @"Paste", @selector(paste:), @"v", NSEventModifierFlagCommand);
	addMenuItem(editMenu, @"Paste and Match Style", @selector(pasteAsPlainText:), @"v", NSEventModifierFlagCommand | NSEventModifierFlagOption | NSEventModifierFlagShift);
	[editMenu addItem:[NSMenuItem separatorItem]];
	addMenuItem(editMenu, @"Select All", @selector(selectAll:), @"a", NSEventModifierFlagCommand);
	[editMenuItem setSubmenu:editMenu];

	[NSApp setMainMenu:mainMenu];
}

static void runManagerWindow(const char *titleChars, const char *urlChars) {
	@autoreleasepool {
		NSString *title = [NSString stringWithUTF8String:titleChars];
		NSString *urlString = [NSString stringWithUTF8String:urlChars];
		NSApplication *app = [NSApplication sharedApplication];
		[app setActivationPolicy:NSApplicationActivationPolicyRegular];
		managerWindowDelegate = [[ManagerWindowDelegate alloc] init];
		[app setDelegate:managerWindowDelegate];
		installManagerMenu(title);

		NSRect frame = NSMakeRect(0, 0, 1180, 780);
		NSWindowStyleMask style = NSWindowStyleMaskTitled |
			NSWindowStyleMaskClosable |
			NSWindowStyleMaskMiniaturizable |
			NSWindowStyleMaskResizable;
		NSWindow *window = [[NSWindow alloc] initWithContentRect:frame
			styleMask:style
			backing:NSBackingStoreBuffered
			defer:NO];
		[window setTitle:title];
		[window center];
		[window setReleasedWhenClosed:NO];
		[window setDelegate:managerWindowDelegate];

		WKWebViewConfiguration *configuration = [[WKWebViewConfiguration alloc] init];
		WKWebView *webView = [[WKWebView alloc] initWithFrame:frame configuration:configuration];
		[webView setUIDelegate:managerWindowDelegate];
		[webView setAutoresizingMask:NSViewWidthSizable | NSViewHeightSizable];
		[[window contentView] addSubview:webView];
		[webView loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:urlString]]];

		[window makeKeyAndOrderFront:nil];
		[window makeFirstResponder:webView];
		[app activateIgnoringOtherApps:YES];
		[app run];
	}
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

func defaultManagerDesktop() bool {
	return true
}

func lockManagerDesktopThread() {
	runtime.LockOSThread()
}

func runManagerDesktopWindow(title, url string) error {
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	C.runManagerWindow(cTitle, cURL)
	return nil
}
