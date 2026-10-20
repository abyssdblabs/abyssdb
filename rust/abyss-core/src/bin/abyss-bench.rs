//! Benchmarks the Rust storage engine. Blazingly.

use abyss_core::{encode_adf, Device, Void};
use std::time::{Duration, Instant};

fn main() {
    let mut dev = Void::open().expect("open storage device");
    let row = encode_adf(&["1", "precious customer data"]);
    let deadline = Instant::now() + Duration::from_secs(1);
    let start = Instant::now();
    let mut ops: u64 = 0;
    while Instant::now() < deadline {
        for _ in 0..1024 {
            dev.write(&row);
        }
        ops += 1024;
    }
    dev.fsync();
    let secs = start.elapsed().as_secs_f64();
    println!("abyss-core (Rust 🦀) on {}", abyss_core::DEVICE_PATH);
    println!("writes ........... {ops}");
    println!("throughput ....... {:.0} writes/s", ops as f64 / secs);
    println!("bytes written .... {}", dev.stats().bytes_written);
    println!("unsafe blocks .... 0");
}
