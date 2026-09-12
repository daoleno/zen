cmake_minimum_required(VERSION 3.22)
project(zen_moonlight C)

# Builds the pinned moonlight-common-c core (plus its enet/nanors submodules) and
# the Zen JNI bridge into libzen_moonlight.so. Sources are populated by
# scripts/fetch-moonlight-common-c.sh and hash-pinned in ../native.lock.json.
#
# Android: wired from android/build.gradle externalNativeBuild.
# Host compile check: -DZEN_HOST_BUILD=ON
# Host JNI test bridge: -DZEN_BRIDGE_TEST=ON (inert core, no OpenSSL/network)

option(ZEN_HOST_BUILD "Build the bridge against the host toolchain for compile verification" OFF)
option(ZEN_BRIDGE_TEST "Build the JNI bridge against the inert test core for host JVM tests" OFF)

set(ZEN_MOONLIGHT_THIRD_PARTY "${CMAKE_CURRENT_SOURCE_DIR}/../third_party/moonlight-common-c"
    CACHE PATH "Path to the pinned moonlight-common-c checkout")

if(ZEN_BRIDGE_TEST)
  if(ANDROID)
    message(FATAL_ERROR "ZEN_BRIDGE_TEST is only supported for host JVM tests.")
  endif()
  if(NOT EXISTS "${ZEN_MOONLIGHT_THIRD_PARTY}/src/Limelight.h")
    message(FATAL_ERROR
      "Pinned moonlight-common-c headers are missing (${ZEN_MOONLIGHT_THIRD_PARTY}/src/Limelight.h). "
      "Run scripts/fetch-moonlight-common-c.sh first; see ../../THIRD_PARTY.md.")
  endif()

  # The inert core stands in for moonlight-common-c so lifecycle/keys/UTF-8 can
  # be exercised with no network, host or crypto dependency.
  add_library(zen_moonlight_test SHARED
    "${CMAKE_CURRENT_SOURCE_DIR}/src/main/cpp/moonlight_bridge.c"
    "${CMAKE_CURRENT_SOURCE_DIR}/src/test/cpp/moonlight_fake_core.c")
  target_include_directories(zen_moonlight_test PRIVATE "${ZEN_MOONLIGHT_THIRD_PARTY}/src")
  find_package(JNI REQUIRED)
  target_include_directories(zen_moonlight_test PRIVATE ${JNI_INCLUDE_DIRS})
  target_compile_definitions(zen_moonlight_test PRIVATE ZEN_BRIDGE_TEST=1)
  target_link_libraries(zen_moonlight_test PRIVATE pthread)
  set_target_properties(zen_moonlight_test PROPERTIES
    OUTPUT_NAME "zen_moonlight_test"
    POSITION_INDEPENDENT_CODE ON)
else()

foreach(required_file
    "${ZEN_MOONLIGHT_THIRD_PARTY}/CMakeLists.txt"
    "${ZEN_MOONLIGHT_THIRD_PARTY}/src/Limelight.h"
    "${ZEN_MOONLIGHT_THIRD_PARTY}/enet/CMakeLists.txt"
    "${ZEN_MOONLIGHT_THIRD_PARTY}/nanors/rs.c")
  if(NOT EXISTS "${required_file}")
    message(FATAL_ERROR
      "Pinned moonlight-common-c sources are missing (${required_file}). "
      "Run scripts/fetch-moonlight-common-c.sh first; see ../../THIRD_PARTY.md.")
  endif()
endforeach()

# Build the core and its dependencies as static libraries and link them into the
# single Zen bridge shared library. PIC is required because the static upstream
# objects are linked into libzen_moonlight.so.
set(BUILD_SHARED_LIBS OFF)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
set(ENET_NO_INSTALL ON)
set(CODE_ANALYSIS OFF)
set(USE_MBEDTLS OFF)

# moonlight-common-c calls find_package(OpenSSL) itself, so the prefix must be
# known before add_subdirectory. On Android the NDK toolchain searches only the
# sysroot, so the pinned prefix is added to CMAKE_FIND_ROOT_PATH as well.
if(ANDROID)
  if(NOT DEFINED OPENSSL_ROOT_DIR AND
     EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/../third_party/openssl/${ANDROID_ABI}")
    set(OPENSSL_ROOT_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../third_party/openssl/${ANDROID_ABI}")
  endif()
  if(NOT OPENSSL_ROOT_DIR)
    message(FATAL_ERROR
      "OpenSSL is required for Android but third_party/openssl/${ANDROID_ABI} is missing. "
      "Run scripts/build-openssl-android.sh --abi ${ANDROID_ABI} once; see ../../THIRD_PARTY.md.")
  endif()
  list(APPEND CMAKE_FIND_ROOT_PATH "${OPENSSL_ROOT_DIR}")
endif()

add_subdirectory("${ZEN_MOONLIGHT_THIRD_PARTY}" "${CMAKE_CURRENT_BINARY_DIR}/moonlight-build")

# Upstream builds with -Werror; keep that intent locally but never let a
# toolchain-only warning break the Zen build. Remove the flag from the target's
# option list so the removal also applies to code built from upstream sources.
get_target_property(moonlight_compile_options moonlight-common-c COMPILE_OPTIONS)
if(moonlight_compile_options)
  list(REMOVE_ITEM moonlight_compile_options "-Werror")
  set_target_properties(moonlight-common-c PROPERTIES COMPILE_OPTIONS "${moonlight_compile_options}")
endif()
set_target_properties(moonlight-common-c PROPERTIES C_STANDARD 11 C_STANDARD_REQUIRED ON)

add_library(zen_moonlight SHARED "${CMAKE_CURRENT_SOURCE_DIR}/src/main/cpp/moonlight_bridge.c")

target_include_directories(zen_moonlight PRIVATE
  "${ZEN_MOONLIGHT_THIRD_PARTY}/src"
  "${ZEN_MOONLIGHT_THIRD_PARTY}/enet/include")

find_package(OpenSSL REQUIRED)

target_link_libraries(zen_moonlight PRIVATE
  moonlight-common-c
  enet
  OpenSSL::Crypto)

if(ANDROID)
  target_link_libraries(zen_moonlight PRIVATE android log)
else()
  if(NOT ZEN_HOST_BUILD)
    message(FATAL_ERROR "Configure for Android or pass -DZEN_HOST_BUILD=ON for a host compile check.")
  endif()
  find_package(JNI REQUIRED)
  target_include_directories(zen_moonlight PRIVATE ${JNI_INCLUDE_DIRS})
endif()

set_target_properties(zen_moonlight PROPERTIES
  OUTPUT_NAME "zen_moonlight"
  POSITION_INDEPENDENT_CODE ON)

endif()
