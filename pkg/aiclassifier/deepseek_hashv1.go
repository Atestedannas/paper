package aiclassifier

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

var (
	wasmBytes     []byte
	wasmBytesOnce sync.Once
)

func getWasmBytes() []byte {
	wasmBytesOnce.Do(func() {
		var err error
		wasmBytes, err = base64.StdEncoding.DecodeString(sha3WasmB64)
		if err != nil {
			log.Printf("[DeepSeek] WARNING: failed to decode WASM base64: %v", err)
		}
	})
	return wasmBytes
}

func solveDeepSeekHashV1(challenge, salt string, difficulty int, expireAt int64) (float64, error) {
	wb := getWasmBytes()
	if wb == nil {
		return 0, fmt.Errorf("WASM bytes not available")
	}

	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	_, err := r.NewHostModuleBuilder("wbg").Instantiate(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to create wbg host module: %w", err)
	}

	mod, err := r.Instantiate(ctx, wb)
	if err != nil {
		return 0, fmt.Errorf("failed to instantiate WASM module: %w", err)
	}

	alloc := mod.ExportedFunction("__wbindgen_export_0")
	addToStack := mod.ExportedFunction("__wbindgen_add_to_stack_pointer")
	wasmSolve := mod.ExportedFunction("wasm_solve")

	if alloc == nil || addToStack == nil || wasmSolve == nil {
		return 0, fmt.Errorf("WASM module missing required exports (alloc=%v, addToStack=%v, wasmSolve=%v)",
			alloc != nil, addToStack != nil, wasmSolve != nil)
	}

	prefix := fmt.Sprintf("%s_%d_", salt, expireAt)
	log.Printf("[DeepSeek] DeepSeekHashV1 solving:\n"+
		"  challenge : %s...\n"+
		"  prefix    : %s\n"+
		"  difficulty: %d (f64 bits: %016x)",
		challenge[:min(len(challenge), 32)], prefix, difficulty, api.EncodeF64(float64(difficulty)))

	encodeString := func(s string) (uint32, uint32, error) {
		buf := []byte(s)
		results, err := alloc.Call(ctx, uint64(len(buf)), 1)
		if err != nil {
			return 0, 0, fmt.Errorf("alloc failed: %w", err)
		}
		ptr := uint32(results[0])
		if !mod.Memory().Write(ptr, buf) {
			return 0, 0, fmt.Errorf("failed to write %d bytes to WASM memory at offset %d", len(buf), ptr)
		}
		return ptr, uint32(len(buf)), nil
	}

	ptrC, lenC, err := encodeString(challenge)
	if err != nil {
		return 0, fmt.Errorf("encode challenge: %w", err)
	}
	ptrP, lenP, err := encodeString(prefix)
	if err != nil {
		return 0, fmt.Errorf("encode prefix: %w", err)
	}

	retResults, err := addToStack.Call(ctx, api.EncodeI32(-16))
	if err != nil {
		return 0, fmt.Errorf("add_to_stack_pointer(-16) failed: %w", err)
	}
	retptr := uint32(retResults[0])

	start := time.Now()
	_, err = wasmSolve.Call(ctx,
		uint64(retptr), uint64(ptrC), uint64(lenC),
		uint64(ptrP), uint64(lenP), api.EncodeF64(float64(difficulty)))
	elapsed := time.Since(start)
	if err != nil {
		return 0, fmt.Errorf("wasm_solve failed: %w", err)
	}

	statusBytes, ok := mod.Memory().Read(retptr, 4)
	if !ok {
		return 0, fmt.Errorf("failed to read status from WASM memory")
	}
	status := int32(binary.LittleEndian.Uint32(statusBytes))

	answerBytes, ok := mod.Memory().Read(retptr+8, 8)
	if !ok {
		return 0, fmt.Errorf("failed to read answer from WASM memory")
	}
	answer := math.Float64frombits(binary.LittleEndian.Uint64(answerBytes))

	_, _ = addToStack.Call(ctx, api.EncodeI32(16))

	if status == 0 {
		return 0, fmt.Errorf("DeepSeekHashV1 WASM solver returned status=0 (no solution found)")
	}

	log.Printf("[DeepSeek] DeepSeekHashV1 solved:\n"+
		"  answer   : %v\n"+
		"  elapsed  : %v\n"+
		"  prefix   : %s\n"+
		"  difficulty: %d",
		answer, elapsed, prefix, difficulty)

	return answer, nil
}
