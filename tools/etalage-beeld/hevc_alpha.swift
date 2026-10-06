// HEVC met alfa voor Safari, via AVAssetWriter (dezelfde VideoToolbox-encoder
// als ffmpegs hevc_videotoolbox), maar mét kleurlabels in de stream. ffmpeg
// schrijft bij BGRA-invoer 'unspecified' in de VUI en een remux om labels toe
// te voegen breekt de alfalaag; WebKit toonde de ongelabelde clip ~5 niveaus
// lichter dan rust.webp, een zichtbare sprong bij het wisselen. build.py
// compileert dit bestand één keer naar out/bin/hevc_alpha.
//
//   hevc_alpha <map met 000.png…> <fps> <uit.mov> <kwaliteit 0–1 of bitrate in bit/s> <alfakwaliteit 0–1> [srgb|709] [straight|premul]
import AVFoundation
import CoreImage
import Foundation
import ImageIO
import VideoToolbox

let args = CommandLine.arguments
guard (6...8).contains(args.count), let fps = Int32(args[2]), let quality = Double(args[4]), let alphaQuality = Double(args[5]) else {
    FileHandle.standardError.write("gebruik: hevc_alpha <map> <fps> <uit.mov> <kwaliteit> <alfakwaliteit>\n".data(using: .utf8)!)
    exit(2)
}
let straight = !(args.count == 8 && args[7] == "premul")
let dir = URL(fileURLWithPath: args[1])
let out = URL(fileURLWithPath: args[3])
let files = try FileManager.default.contentsOfDirectory(atPath: dir.path).filter { $0.hasSuffix(".png") }.sorted()
guard let first = files.first,
      let src0 = CGImageSourceCreateWithURL(dir.appendingPathComponent(first) as CFURL, nil),
      let img0 = CGImageSourceCreateImageAtIndex(src0, 0, nil) else { fatalError("geen beelden in \(dir.path)") }
let width = img0.width, height = img0.height
try? FileManager.default.removeItem(at: out)

let writer = try AVAssetWriter(outputURL: out, fileType: .mov)
writer.shouldOptimizeForNetworkUse = true   // moov-atoom vooraan: afspelen begint zonder het hele bestand
let colour: [String: Any] = [
    AVVideoColorPrimariesKey: AVVideoColorPrimaries_ITU_R_709_2,
    AVVideoTransferFunctionKey: (args.count == 7 && args[6] == "709") ? AVVideoTransferFunction_ITU_R_709_2 : AVVideoTransferFunction_IEC_sRGB,
    AVVideoYCbCrMatrixKey: AVVideoYCbCrMatrix_ITU_R_709_2,
]
let settings: [String: Any] = [
    AVVideoCodecKey: AVVideoCodecType.hevcWithAlpha,
    AVVideoWidthKey: width,
    AVVideoHeightKey: height,
    AVVideoColorPropertiesKey: colour,
    // kwaliteit ≤ 1: VideoToolbox-kwaliteit; > 1: gemiddelde bitrate in bit/s.
    AVVideoCompressionPropertiesKey: (quality > 1 ? [AVVideoAverageBitRateKey: Int(quality)] : [AVVideoQualityKey: quality]).merging([
        kVTCompressionPropertyKey_TargetQualityForAlpha as String: alphaQuality,
        AVVideoExpectedSourceFrameRateKey: fps,
        AVVideoMaxKeyFrameIntervalKey: 1000,
        AVVideoAllowFrameReorderingKey: false,
        kVTCompressionPropertyKey_AlphaChannelMode as String: straight ? kVTAlphaChannelMode_StraightAlpha : kVTAlphaChannelMode_PremultipliedAlpha,
    ] as [String: Any]) { a, _ in a },
]
let input = AVAssetWriterInput(mediaType: .video, outputSettings: settings)
input.expectsMediaDataInRealTime = false
let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [
    kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
    kCVPixelBufferWidthKey as String: width,
    kCVPixelBufferHeightKey as String: height,
])
writer.add(input)
guard writer.startWriting() else { fatalError("\(writer.error!)") }
writer.startSession(atSourceTime: .zero)

// De PNG's zijn sRGB met rechte (niet-voorvermenigvuldigde) alfa; tekenen in
// een sRGB-context met voorvermenigvuldigde BGRA is wat de encoder verwacht.
let srgb = CGColorSpace(name: CGColorSpace.sRGB)!
for (i, name) in files.enumerated() {
    guard let src = CGImageSourceCreateWithURL(dir.appendingPathComponent(name) as CFURL, nil),
          let img = CGImageSourceCreateImageAtIndex(src, 0, nil) else { fatalError("onleesbaar: \(name)") }
    while !input.isReadyForMoreMediaData { usleep(2000) }
    var buffer: CVPixelBuffer?
    CVPixelBufferPoolCreatePixelBuffer(nil, adaptor.pixelBufferPool!, &buffer)
    guard let pb = buffer else { fatalError("geen buffer") }
    CVBufferSetAttachment(pb, kCVImageBufferCGColorSpaceKey, srgb, .shouldPropagate)
    CVPixelBufferLockBaseAddress(pb, [])
    let ctx = CGContext(data: CVPixelBufferGetBaseAddress(pb), width: width, height: height, bitsPerComponent: 8,
                        bytesPerRow: CVPixelBufferGetBytesPerRow(pb), space: srgb,
                        bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue)!
    ctx.clear(CGRect(x: 0, y: 0, width: width, height: height))
    ctx.draw(img, in: CGRect(x: 0, y: 0, width: width, height: height))
    if straight {   // CoreGraphics tekent voorvermenigvuldigd; terug naar rechte alfa
        let base = CVPixelBufferGetBaseAddress(pb)!.assumingMemoryBound(to: UInt8.self)
        let stride = CVPixelBufferGetBytesPerRow(pb)
        for y in 0..<height {
            let row = base + y * stride
            for x in 0..<width {
                let p = row + x * 4, a = Int(p[3])
                if a > 0 && a < 255 {
                    p[0] = UInt8(min(255, (Int(p[0]) * 255 + a / 2) / a))
                    p[1] = UInt8(min(255, (Int(p[1]) * 255 + a / 2) / a))
                    p[2] = UInt8(min(255, (Int(p[2]) * 255 + a / 2) / a))
                }
            }
        }
    }
    CVPixelBufferUnlockBaseAddress(pb, [])
    adaptor.append(pb, withPresentationTime: CMTime(value: CMTimeValue(i), timescale: fps))
}
input.markAsFinished()
let done = DispatchSemaphore(value: 0)
writer.finishWriting { done.signal() }
done.wait()
if writer.status != .completed { fatalError("\(writer.error!)") }
print("ok \(files.count) beelden \(width)x\(height)")
