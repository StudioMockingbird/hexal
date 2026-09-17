package compiler

// RuntimeABIVersion is the generated/runtime ABI that checked-in runtime packs
// must match. Generated C and the runtime components agree on this version;
// an incompatible generated-runtime change increments it and requires
// refreshed target packs in the same change. The driver compares a pack
// manifest's runtime_abi_version with this value, and no user setting can
// override it.
const RuntimeABIVersion uint32 = 1
