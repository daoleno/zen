cmake_minimum_required(VERSION 3.22.1)
project(zen_terminal_vt)

# Pre-built libghostty_vt shared library
add_library(ghostty_vt SHARED IMPORTED)
set_target_properties(ghostty_vt PROPERTIES
    IMPORTED_LOCATION "${CMAKE_CURRENT_SOURCE_DIR}/../libs/android/${ANDROID_ABI}/libghostty_vt.so"
)

# JNI bridge
add_library(${CMAKE_PROJECT_NAME} SHARED
    src/main/cpp/jni_bridge.cpp
)

target_include_directories(${CMAKE_PROJECT_NAME} PRIVATE
    src/main/cpp     # Contains ghostty/ headers directory
)

target_link_libraries(${CMAKE_PROJECT_NAME}
    ghostty_vt
    android
    log
)
