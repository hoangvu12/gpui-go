//! Fixture `image-codecs-v1` (fx-0008, ticket17's recorded harness
//! follow-up): reference image decoding through the PINNED image crate
//! exactly as the gpui-go native image service drives it.
//!
//! ## The public oracle boundary
//!
//! The reference decode surface mirrors the native image service's
//! pinned paths (`reference/native/src/image.rs`, which follows the CE
//! pin's decode graph):
//!
//! * **Format sniffing** — `image::guess_format` on the raw bytes (the
//!   resource entry's never-an-extension-list rule).
//! * **Animated frame walks** — GIF and animated WebP through
//!   `image::AnimationDecoder` (per-frame delays as the image crate's
//!   rational milliseconds, per-frame rgba8 buffers).
//! * **Static decode** — every other supported format through
//!   `image::ImageReader` with `orientation()` read from the decoder
//!   and `apply_orientation` applied (the EXIF rule), then rgba8.
//! * **Output** — every frame's pixels as BGRA (the port's atlas
//!   upload representation; the reference converts the image crate's
//!   rgba8 by swapping R and B) with a SHA-256 over the exact bytes.
//!
//! The trace events are byte-deterministic (no environment, no fonts,
//! no GPU): the Go conformance test reproduces them through the native
//! image service (DecodeResource / FrameCount / FrameInfo /
//! FramePixels) and compares bit-exactly via `conformance.CompareTraces`.
//!
//! ## Input encoding
//!
//! The envelope carries every case's image bytes as a hex string
//! (self-contained: the same bytes feed the reference harness and the
//! Go native service; the envelope never carries expected outputs).

use std::io::Cursor;

use anyhow::{bail, Context as _, Result};
use image::{AnimationDecoder, ImageDecoder};

use crate::envelope::Envelope;
use crate::trace::{FieldValue, TraceEvent, TraceRecorder};

/// The bytes of one image case, hex-encoded in the envelope.
fn decode_hex(value: &str) -> Result<Vec<u8>> {
    if value.len() % 2 != 0 {
        bail!("odd hex length");
    }
    let mut out = Vec::with_capacity(value.len() / 2);
    let bytes = value.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        let hi = (bytes[i] as char).to_digit(16).context("hex digit")? as u8;
        let lo = (bytes[i + 1] as char).to_digit(16).context("hex digit")? as u8;
        out.push((hi << 4) | lo);
        i += 2;
    }
    Ok(out)
}

fn sha256_hex(bytes: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    let digest = hasher.finalize();
    let mut out = String::with_capacity(64);
    for byte in digest {
        out.push_str(&format!("{:02x}", byte));
    }
    out
}

/// One decoded frame's BGRA bytes: the rgba8 buffer with R and B
/// swapped per pixel (the native service's output representation).
fn bgra_of_rgba(rgba: &image::RgbaImage) -> Vec<u8> {
    let mut out = rgba.as_raw().clone();
    for pixel in out.chunks_exact_mut(4) {
        pixel.swap(0, 2);
    }
    out
}

fn field_str(name: &str, value: &str) -> (String, FieldValue) {
    (name.to_string(), FieldValue::str(value))
}

fn field_u64(name: &str, value: u64) -> (String, FieldValue) {
    (name.to_string(), FieldValue::Uint(value))
}

/// The format tag the trace records (the image crate's
/// ImageFormat Debug name, lowercased).
fn format_tag(format: image::ImageFormat) -> &'static str {
    match format {
        image::ImageFormat::Png => "png",
        image::ImageFormat::Jpeg => "jpeg",
        image::ImageFormat::Gif => "gif",
        image::ImageFormat::WebP => "webp",
        image::ImageFormat::Bmp => "bmp",
        image::ImageFormat::Pnm => "pnm",
        image::ImageFormat::Tiff => "tiff",
        image::ImageFormat::Tga => "tga",
        image::ImageFormat::Dds => "dds",
        _ => "other",
    }
}

pub fn run(envelope: &Envelope) -> Result<Vec<TraceEvent>> {
    let recorder = TraceRecorder::new();
    let Some(inputs) = envelope.inputs.image_cases.as_ref() else {
        bail!("image-codecs-v1 envelope carries no image_cases");
    };
    for case in &inputs.cases {
        let bytes = decode_hex(&case.bytes)?;
        recorder.event_with("image-case", Some(&case.label), [field_str("label", &case.label)]);

        let format = match image::guess_format(&bytes) {
            Ok(format) => format,
            Err(error) => {
                recorder.event_with("sniff-failed", Some(&case.label), [field_str(
                    "error",
                    &format!("{error:?}"),
                )]);
                continue;
            }
        };
        recorder.event_with("format", Some(&case.label), [field_str("format", format_tag(format))]);

        let cursor = Cursor::new(bytes.clone());
        let animated = matches!(format, image::ImageFormat::Gif | image::ImageFormat::WebP)
            && is_animated(format, &bytes);

        if animated {
            // The pinned animated frame walk (GIF and animated WebP).
            let frames: Vec<image::Frame> = match format {
                image::ImageFormat::Gif => {
                    let decoder = image::codecs::gif::GifDecoder::new(cursor)?;
                    AnimationDecoder::into_frames(decoder).collect_frames()?
                }
                _ => {
                    let decoder = image::codecs::webp::WebPDecoder::new(cursor)?;
                    AnimationDecoder::into_frames(decoder).collect_frames()?
                }
            };
            recorder.event_with(
                "decoded",
                Some(&case.label),
                [
                    field_u64("frames", frames.len() as u64),
                ],
            );
            for (index, frame) in frames.iter().enumerate() {
                let (delay_numer, delay_denom) = frame.delay().numer_denom_ms();
                let bgra = bgra_of_rgba(frame.buffer());
                recorder.event_with(
                    "frame",
                    Some(&case.label),
                    [
                        field_u64("index", index as u64),
                        field_u64("width", frame.buffer().width() as u64),
                        field_u64("height", frame.buffer().height() as u64),
                        field_u64("delayNumerMS", delay_numer as u64),
                        field_u64("delayDenomMS", delay_denom as u64),
                        field_str("bgraSHA256", &sha256_hex(&bgra)),
                    ],
                );
            }
        } else {
            // The pinned static decode: EXIF orientation applied, rgba8.
            let reader = image::ImageReader::with_format(Cursor::new(bytes), format);
            let mut decoder = reader.into_decoder()?;
            let orientation = decoder.orientation()?;
            let mut dynamic = image::DynamicImage::from_decoder(decoder)?;
            dynamic.apply_orientation(orientation);
            let rgba = dynamic.to_rgba8();
            recorder.event_with(
                "decoded",
                Some(&case.label),
                [
                    field_u64("width", rgba.width() as u64),
                    field_u64("height", rgba.height() as u64),
                    field_u64("frames", 1),
                ],
            );
            let bgra = bgra_of_rgba(&rgba);
            recorder.event_with(
                "frame",
                Some(&case.label),
                [
                    field_u64("index", 0),
                    field_u64("width", rgba.width() as u64),
                    field_u64("height", rgba.height() as u64),
                    field_u64("delayNumerMS", 0),
                    field_u64("delayDenomMS", 0),
                    field_str("bgraSHA256", &sha256_hex(&bgra)),
                ],
            );
        }
    }
    Ok(recorder.into_events())
}

/// is_animated probes whether the WebP/GIF bytes carry animation (the
/// native service's format-classification rule: GIF and WebP take the
/// animated walk when animated, the static walk otherwise — GIF is
/// always walked animated per the pin's resource path).
fn is_animated(format: image::ImageFormat, bytes: &[u8]) -> bool {
    let cursor = Cursor::new(bytes.to_vec());
    match format {
        image::ImageFormat::Gif => true,
        image::ImageFormat::WebP => {
            match image::codecs::webp::WebPDecoder::new(cursor) {
                Ok(decoder) => decoder.has_animation(),
                Err(_) => false,
            }
        }
        _ => false,
    }
}
