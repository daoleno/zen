#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

/// ObjC++ ownership boundary around the pinned libghostty-vt C ABI.
/// Every method is synchronous and serialized. Native pointers never leave this object.
@interface ZenTerminalVtBridge : NSObject

- (NSDictionary<NSString *, id> *)createTerminalWithColumns:(NSInteger)columns
                                                       rows:(NSInteger)rows NS_SWIFT_NAME(createTerminal(columns:rows:));
- (void)destroyTerminal:(NSInteger)handle NS_SWIFT_NAME(destroyTerminal(handle:));
- (NSDictionary<NSString *, id> *)writeData:(NSString *)data handle:(NSInteger)handle NS_SWIFT_NAME(writeData(_:handle:));
- (NSDictionary<NSString *, id> *)scrollViewport:(NSInteger)delta handle:(NSInteger)handle NS_SWIFT_NAME(scrollViewport(_:handle:));
- (NSDictionary<NSString *, id> *)scrollViewportToBottom:(NSInteger)handle NS_SWIFT_NAME(scrollViewportToBottom(handle:));
- (NSDictionary<NSString *, id> *)resizeTerminal:(NSInteger)handle
                                          columns:(NSInteger)columns
                                             rows:(NSInteger)rows
                                        cellWidth:(double)cellWidth
                                       cellHeight:(double)cellHeight NS_SWIFT_NAME(resize(handle:columns:rows:cellWidth:cellHeight:));
- (NSDictionary<NSString *, id> *)setThemeForTerminal:(NSInteger)handle
                                           foreground:(NSString *)foreground
                                           background:(NSString *)background
                                               cursor:(NSString *)cursor
                                              palette:(NSArray<NSString *> *)palette NS_SWIFT_NAME(setTheme(handle:foreground:background:cursor:palette:));
- (NSDictionary<NSString *, id> *)encodeMouseEventForTerminal:(NSInteger)handle
                                                        action:(NSInteger)action
                                                        button:(NSInteger)button
                                                             x:(double)x
                                                             y:(double)y
                                                          mods:(NSInteger)mods
                                              anyButtonPressed:(BOOL)anyButtonPressed NS_SWIFT_NAME(encodeMouseEvent(handle:action:button:x:y:mods:anyButtonPressed:));
- (NSDictionary<NSString *, id> *)renderSnapshotForTerminal:(NSInteger)handle NS_SWIFT_NAME(renderSnapshot(handle:));
- (NSDictionary<NSString *, id> *)visibleTextForTerminal:(NSInteger)handle NS_SWIFT_NAME(visibleText(handle:));
- (NSDictionary<NSString *, id> *)visibleHTMLForTerminal:(NSInteger)handle NS_SWIFT_NAME(visibleHTML(handle:));

@end

NS_ASSUME_NONNULL_END
