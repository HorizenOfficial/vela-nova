module github.com/HorizenOfficial/vela-nova/payment-app

// sets the minimum language version required for the module
go 1.24.0

// This prevents "works on my machine" problems that can arise from subtle differences between minor Go versions
toolchain go1.24.3

// Can be useful for local developments
//replace github.com/HorizenOfficial/vela => ../../../vela
//replace github.com/HorizenOfficial/vela-common-go => ../../../vela-common-go

require (
	github.com/HorizenOfficial/vela v0.0.25
	github.com/HorizenOfficial/vela-common-go v0.0.26
	github.com/ethereum/go-ethereum v1.16.1
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/aws/aws-sdk-go-v2 v1.41.1 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.29.14 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.67 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/kms v1.49.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.30.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.19 // indirect
	github.com/aws/smithy-go v1.24.0 // indirect
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/bytecodealliance/wasmtime-go v1.0.0 // indirect
	github.com/consensys/gnark-crypto v0.18.0 // indirect
	github.com/crate-crypto/go-eth-kzg v1.3.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/elliotchance/orderedmap/v3 v3.1.0 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.0 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/fxamacker/cbor/v2 v2.2.0 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.4 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/golang/snappy v0.0.5-0.20220116011046-fa5810519dcb // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/hf/nsm v0.0.0-20220930140112-cd181bd646b9 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mdlayher/socket v0.4.1 // indirect
	github.com/mdlayher/vsock v1.2.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible // indirect
	github.com/supranational/blst v0.3.14 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/crypto v0.39.0 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sync v0.15.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
