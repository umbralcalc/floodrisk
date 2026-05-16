//go:build js && wasm

// register_step is the floodrisk widget compiled as a WebAssembly
// module. It registers `stepSimulation` on the JS global and blocks
// forever so the Go runtime stays alive to service per-step calls
// from dexetera's runtime/worker.js.
//
// Build with the codegen-emitted app/flood/build.sh or directly:
//
//	GOOS=js GOARCH=wasm go build -o app/flood/src/main.wasm \
//	    ./app/cmd/flood/register_step
package main

import (
	"github.com/umbralcalc/dexetera/pkg/simio"
	"github.com/umbralcalc/floodrisk/app/pkg/flooddash"
)

func main() {
	simio.RegisterStep(flooddash.NewConfig())
}
