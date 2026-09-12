package expo.modules.zenremotedesktop.moonlight

/**
 * Upstream video format constants. VIDEO_FORMAT_* are the bits a client puts in
 * STREAM_CONFIGURATION.supportedVideoFormats; SCM_* are the host's
 * ServerCodecModeSupport bits. They are different masks (for example
 * VIDEO_FORMAT_AV1_MAIN8 = 0x1000 while SCM_AV1_MAIN8 = 0x10000) and must
 * never be intersected with each other.
 *
 * Values mirror third_party/moonlight-common-c/src/Limelight.h at the pinned
 * commit.
 */
object MoonlightVideoFormats {
    const val H264 = 0x0001
    const val H264_HIGH8_444 = 0x0004
    const val H265 = 0x0100
    const val H265_MAIN10 = 0x0200
    const val H265_REXT8_444 = 0x0400
    const val H265_REXT10_444 = 0x0800
    const val AV1_MAIN8 = 0x1000
    const val AV1_MAIN10 = 0x2000
    const val AV1_HIGH8_444 = 0x4000
    const val AV1_HIGH10_444 = 0x8000

    const val MASK_H264 = 0x000F
    const val MASK_H265 = 0x0F00
    const val MASK_AV1 = 0xF000
    const val MASK_10BIT = 0xAA00
    const val MASK_YUV444 = 0xCC04

    const val SCM_H264 = 0x00000001
    const val SCM_HEVC = 0x00000100
    const val SCM_HEVC_MAIN10 = 0x00000200
    const val SCM_AV1_MAIN8 = 0x00010000
    const val SCM_AV1_MAIN10 = 0x00020000
    const val SCM_H264_HIGH8_444 = 0x00040000
    const val SCM_HEVC_REXT8_444 = 0x00080000
    const val SCM_HEVC_REXT10_444 = 0x00100000
    const val SCM_AV1_HIGH8_444 = 0x00200000
    const val SCM_AV1_HIGH10_444 = 0x00400000

    const val SCM_MASK_H264 = SCM_H264 or SCM_H264_HIGH8_444
    const val SCM_MASK_HEVC = SCM_HEVC or SCM_HEVC_MAIN10 or SCM_HEVC_REXT8_444 or SCM_HEVC_REXT10_444
    const val SCM_MASK_AV1 = SCM_AV1_MAIN8 or SCM_AV1_MAIN10 or SCM_AV1_HIGH8_444 or SCM_AV1_HIGH10_444
    const val SCM_MASK_10BIT = SCM_HEVC_MAIN10 or SCM_HEVC_REXT10_444 or SCM_AV1_MAIN10 or SCM_AV1_HIGH10_444

    /** Formats the current Android renderer actually decodes. H.264 only. */
    const val DECODER_SUPPORTED = H264

    private const val KNOWN = MASK_H264 or MASK_H265 or MASK_AV1

    /** True when the mask is non-empty, known, and includes H.264. */
    fun isRenderable(formats: Int): Boolean =
        formats != 0 && (formats and KNOWN.inv()) == 0 && (formats and MASK_H264) != 0
}
