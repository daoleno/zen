require 'json'

package = JSON.parse(File.read(File.join(__dir__, 'package.json')))

Pod::Spec.new do |s|
  s.name           = 'ZenTerminalVt'
  s.version        = package['version']
  s.summary        = 'Zen libghostty-vt terminal bridge for iOS'
  s.description    = 'Expo module exposing the same Ghostty VT core used by the Android terminal surface.'
  s.license        = 'Apache-2.0'
  s.author         = 'Zen contributors'
  s.homepage       = 'https://github.com/daoleno/zen'
  s.platforms      = { :ios => '16.4' }
  s.source         = { :path => '.' }
  s.static_framework = true

  s.source_files        = 'ios/**/*.{h,mm,swift}'
  s.public_header_files = 'ios/**/*.h'
  s.vendored_frameworks = 'libs/ios/GhosttyVt.xcframework'
  s.preserve_paths      = 'libs/ios/GhosttyVt.xcframework'

  s.dependency 'ExpoModulesCore'
  s.libraries = 'c++'
  s.pod_target_xcconfig = {
    'CLANG_CXX_LANGUAGE_STANDARD' => 'c++20',
    'DEFINES_MODULE' => 'YES',
  }
end
