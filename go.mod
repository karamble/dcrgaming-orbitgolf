module github.com/karamble/dcrgaming-orbitgolf

go 1.26.0

require (
	github.com/decred/dcrd/chaincfg/v3 v3.2.1
	github.com/decred/dcrd/crypto/blake256 v1.1.0
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0
	github.com/decred/dcrd/txscript/v4 v4.1.1
	github.com/decred/slog v1.2.0
	github.com/karamble/dcrgaming-sdk v0.0.0
)

require (
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/decred/base58 v1.0.5 // indirect
	github.com/decred/dcrd/chaincfg/chainhash v1.0.5 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/dcrec v1.0.1 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3 // indirect
	github.com/decred/dcrd/wire v1.7.0 // indirect
	github.com/klauspost/cpuid/v2 v2.0.9 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	lukechampine.com/blake3 v1.3.0 // indirect
)

replace github.com/karamble/dcrgaming-sdk => ../dcrgaming-sdk
