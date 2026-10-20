//! WebAssembly ABI for `abyss-core`, consumed by the Go server via wazero.
//!
//! The host never hands Rust a raw pointer to dereference. Instead, Rust
//! allocates a staging buffer, hands its address to the host, and reads
//! staged data back through a safe slice. There are no `unsafe` blocks in
//! this crate; the exports only carry `#[no_mangle]`.
//!
//! Exports:
//! - `abyss_open() -> u32`: opens a device, returns a handle
//! - `abyss_buffer(handle, len) -> u32`: address of a staging buffer of `len` bytes
//! - `abyss_write(handle, len) -> u32`: writes `len` staged bytes, returns bytes written
//! - `abyss_read_all(handle) -> u32`: full scan, returns bytes read
//! - `abyss_fsync(handle)`
//! - `abyss_close(handle)`

use abyss_core::{Device, Void};
use std::sync::Mutex;

struct Slot {
    device: Void,
    staging: Vec<u8>,
}

static SLOTS: Mutex<Vec<Option<Slot>>> = Mutex::new(Vec::new());

fn with_slot<R>(handle: u32, f: impl FnOnce(&mut Slot) -> R) -> Option<R> {
    let mut slots = SLOTS.lock().ok()?;
    slots.get_mut(handle as usize)?.as_mut().map(f)
}

#[no_mangle]
pub extern "C" fn abyss_open() -> u32 {
    let device = Void::open().expect("open device");
    let mut slots = SLOTS.lock().expect("slots");
    slots.push(Some(Slot { device, staging: Vec::new() }));
    (slots.len() - 1) as u32
}

#[no_mangle]
pub extern "C" fn abyss_buffer(handle: u32, len: u32) -> u32 {
    with_slot(handle, |s| {
        s.staging.resize(len as usize, 0);
        s.staging.as_mut_ptr() as u32
    })
    .unwrap_or(0)
}

#[no_mangle]
pub extern "C" fn abyss_write(handle: u32, len: u32) -> u32 {
    with_slot(handle, |s| {
        let n = (len as usize).min(s.staging.len());
        s.device.write(&s.staging[..n]) as u32
    })
    .unwrap_or(0)
}

#[no_mangle]
pub extern "C" fn abyss_read_all(handle: u32) -> u32 {
    with_slot(handle, |s| s.device.read_all().len() as u32).unwrap_or(0)
}

#[no_mangle]
pub extern "C" fn abyss_fsync(handle: u32) {
    with_slot(handle, |s| s.device.fsync());
}

#[no_mangle]
pub extern "C" fn abyss_close(handle: u32) {
    if let Ok(mut slots) = SLOTS.lock() {
        if let Some(slot) = slots.get_mut(handle as usize) {
            *slot = None;
        }
    }
}
