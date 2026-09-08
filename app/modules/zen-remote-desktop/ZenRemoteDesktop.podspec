require 'json'
package = JSON.parse(File.read(File.join(__dir__, 'package.json')))
Pod::Spec.new do |s|
  s.name = 'ZenRemoteDesktop'
  s.version = package['version']
  s.summary = 'Native authenticated desktop video for Zen'
  s.description = 'H.264 desktop media stays in the native decoder and renderer.'
  s.license = 'Apache-2.0'
  s.author = 'Zen contributors'
  s.homepage = 'https://github.com/daoleno/zen'
  s.platforms = { :ios => '16.4' }
  s.source = { :path => '.' }
  s.static_framework = true
  s.source_files = 'ios/**/*.swift'
  s.resources = 'notices/IPADDR-MIT.txt'
  s.frameworks = 'AVFoundation', 'CoreMedia', 'VideoToolbox', 'Network'
  s.dependency 'ExpoModulesCore'
  s.dependency 'React-Core'
end
