package specdata

// Target-semantics records arrive here: the per-target facts checking and
// generation consume, such as word widths, endianness, and the threading and
// TLS model. Target identity stays in compiler/types and qualification stays in
// internal/driver, so neither appears in a record.
