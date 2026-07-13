Pod::Spec.new do |s|
  s.name           = 'ZenTerminalVt'
  s.version        = '0.1.0'
  s.summary        = 'Zen terminal bridge backed by pinned libghostty-vt'
  s.description    = 'Expo module and ObjC++ ownership bridge for Zen Terminal on iOS.'
  s.license        = { :type => 'MIT', :file => '../NOTICE.Ghostty' }
  s.author         = 'Zen contributors'
  s.homepage       = 'https://github.com/daoleno/zen'
  s.platforms      = { :ios => '17.0' }
  s.source         = { :git => 'https://github.com/daoleno/zen.git' }
  s.static_framework = true
  s.swift_version  = '5.9'

  s.dependency 'ExpoModulesCore'
  s.source_files = '*.{h,m,mm,swift}'
  # Structural capability gate: pod installation fails when the verified pinned
  # artifact has not been materialized. There is no optional or runtime fallback.
  s.vendored_frameworks = '../libs/apple/ghostty-vt.xcframework'
  s.frameworks = 'Foundation'
  s.pod_target_xcconfig = {
    'CLANG_CXX_LANGUAGE_STANDARD' => 'c++17',
    'DEFINES_MODULE' => 'YES'
  }
end
