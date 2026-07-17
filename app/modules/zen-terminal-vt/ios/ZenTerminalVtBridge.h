#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

@interface ZenTerminalVtBridge : NSObject

+ (unsigned long long)createTerminalWithCols:(NSInteger)cols rows:(NSInteger)rows;
+ (void)destroyTerminal:(unsigned long long)handle;
+ (void)writeData:(NSString *)data toTerminal:(unsigned long long)handle;
+ (void)resizeTerminal:(unsigned long long)handle
                   cols:(NSInteger)cols
                   rows:(NSInteger)rows
              cellWidth:(double)cellWidth
             cellHeight:(double)cellHeight;
+ (BOOL)setThemeForTerminal:(unsigned long long)handle
                  foreground:(NSString *)foreground
                  background:(NSString *)background
                      cursor:(NSString *)cursor
                     palette:(NSArray<NSString *> *)palette;
+ (NSString *)encodeMouseForTerminal:(unsigned long long)handle
                               action:(NSInteger)action
                               button:(NSInteger)button
                                    x:(double)x
                                    y:(double)y
                                 mods:(NSInteger)mods
                     anyButtonPressed:(BOOL)anyButtonPressed;
+ (NSDictionary<NSString *, id> *)renderSnapshotForTerminal:(unsigned long long)handle;
+ (NSString *)visibleHTMLForTerminal:(unsigned long long)handle;

@end

NS_ASSUME_NONNULL_END
