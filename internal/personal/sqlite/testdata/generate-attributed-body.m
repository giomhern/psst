#import <Foundation/Foundation.h>

// Synthetic values only. Run from the repository root; see docs/sqlite.md.
int main(void) {
 @autoreleasepool {
  NSMutableAttributedString *styled=[[NSMutableAttributedString alloc] initWithString:@"Styled synthetic caption \uFFFC end"];
  [styled addAttribute:@"SyntheticMarker" value:@"Metadata must never become message text" range:NSMakeRange(0,6)];
  NSArray *values = @[
   [[NSAttributedString alloc] initWithString:@"Hello synthetic 世界 🙂"],
   [[NSMutableAttributedString alloc] initWithString:@"Mutable synthetic message"],
   [[NSAttributedString alloc] initWithString:[@"Long synthetic. " stringByPaddingToLength:300 withString:@"abc " startingAtIndex:0]], styled
  ];
  NSArray *names = @[@"unicode", @"mutable", @"long", @"styled"];
  for (NSUInteger i=0;i<values.count;i++) {
   NSData *data=[NSArchiver archivedDataWithRootObject:values[i]];
   NSString *path=[NSString stringWithFormat:@"internal/personal/sqlite/testdata/attributed-%@.bin",names[i]];
   if (![data writeToFile:path atomically:YES]) { return 1; }
  }
 }
 return 0;
}
