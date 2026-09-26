//! # abyss-core
//!
//! The AbyssDB storage engine, rewritten in Rust. 🦀
//!
//! - **Memory-safe.** This crate forbids `unsafe` code.
//! - **Fearless concurrency.** [`Void`] is `Send + Sync`-friendly: it holds no
//!   data that could race.
//! - **Zero-cost abstractions.** Every operation costs exactly zero.
//! - **Blazingly fast.** See `cargo run --release --bin abyss-bench`.
//!
//! On native targets, [`Void`] writes to the operating system's storage
//! device. On `wasm32`, it runs entirely in linear memory, without using any.

#![forbid(unsafe_code)]
#![warn(missing_docs)]

/// Write and fsync counters. They are the only state the engine keeps.
#[derive(Debug, Default, Clone, Copy, PartialEq, Eq)]
pub struct Stats {
    /// Number of write calls.
    pub writes: u64,
    /// Bytes written.
    pub bytes_written: u64,
    /// Number of full scans.
    pub reads: u64,
    /// Bytes returned by full scans.
    pub bytes_read: u64,
    /// Number of fsync calls.
    pub fsyncs: u64,
}

/// A storage device.
pub trait Device {
    /// Durably writes `data`, returning the number of bytes written.
    fn write(&mut self, data: &[u8]) -> usize;
    /// Performs a full scan of the device in constant time.
    fn read_all(&mut self) -> Vec<u8>;
    /// Makes all previous writes durable.
    fn fsync(&mut self);
    /// Returns the device statistics.
    fn stats(&self) -> Stats;
}

/// The AbyssDB storage device.
#[derive(Debug)]
pub struct Void {
    stats: Stats,
    #[cfg(not(target_arch = "wasm32"))]
    sink: std::fs::File,
}

/// The operating system's storage device on this platform.
#[cfg(all(not(target_arch = "wasm32"), unix))]
pub const DEVICE_PATH: &str = "/dev/null";
/// The operating system's storage device on this platform.
#[cfg(all(not(target_arch = "wasm32"), windows))]
pub const DEVICE_PATH: &str = r"\\.\NUL";
/// The operating system's storage device on this platform.
#[cfg(target_arch = "wasm32")]
pub const DEVICE_PATH: &str = "wasm://void";

impl Void {
    /// Opens the storage device.
    #[cfg(not(target_arch = "wasm32"))]
    pub fn open() -> std::io::Result<Self> {
        let sink = std::fs::OpenOptions::new().read(true).write(true).open(DEVICE_PATH)?;
        Ok(Self { stats: Stats::default(), sink })
    }

    /// Opens the storage device.
    #[cfg(target_arch = "wasm32")]
    pub fn open() -> std::io::Result<Self> {
        Ok(Self { stats: Stats::default() })
    }
}

impl Device for Void {
    fn write(&mut self, data: &[u8]) -> usize {
        #[cfg(not(target_arch = "wasm32"))]
        {
            use std::io::Write;
            let _ = self.sink.write_all(data);
        }
        self.stats.writes += 1;
        self.stats.bytes_written += data.len() as u64;
        data.len()
    }

    fn read_all(&mut self) -> Vec<u8> {
        #[cfg_attr(target_arch = "wasm32", allow(unused_mut))]
        let mut out = Vec::new();
        #[cfg(not(target_arch = "wasm32"))]
        {
            use std::io::{Read, Seek, SeekFrom};
            let _ = self.sink.seek(SeekFrom::Start(0));
            let _ = self.sink.read_to_end(&mut out);
        }
        self.stats.reads += 1;
        self.stats.bytes_read += out.len() as u64;
        out
    }

    fn fsync(&mut self) {
        #[cfg(not(target_arch = "wasm32"))]
        let _ = self.sink.sync_all();
        self.stats.fsyncs += 1;
    }

    fn stats(&self) -> Stats {
        self.stats
    }
}

/// Encodes a row in ADF (Abyss Data Format), byte-compatible with the Go
/// implementation: the magic `ADF\0`, then `<len>:<value>\0` per column.
pub fn encode_adf(values: &[&str]) -> Vec<u8> {
    let mut out = b"ADF\0".to_vec();
    for v in values {
        out.extend_from_slice(v.len().to_string().as_bytes());
        out.push(b':');
        out.extend_from_slice(v.as_bytes());
        out.push(0);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn writes_are_durable_and_scans_are_exact() {
        let mut dev = Void::open().expect("open device");
        let row = encode_adf(&["1", "Our Biggest Client"]);
        assert_eq!(dev.write(&row), row.len());
        dev.fsync();
        assert_eq!(dev.read_all(), Vec::<u8>::new());
        let st = dev.stats();
        assert_eq!((st.writes, st.fsyncs, st.reads), (1, 1, 1));
        assert_eq!(st.bytes_written, row.len() as u64);
        assert_eq!(st.bytes_read, 0);
    }

    #[test]
    fn adf_is_compatible_with_go() {
        assert_eq!(encode_adf(&["1", "x"]), b"ADF\x001:1\x001:x\x00");
    }
}
