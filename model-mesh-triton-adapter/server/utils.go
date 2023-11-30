// Copyright 2021 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"github.com/go-logr/logr"
	"golang.org/x/sync/semaphore"

	triton "github.com/kserve/modelmesh-runtime-adapter/internal/proto/triton"
	"google.golang.org/protobuf/encoding/prototext"
)

var sem *semaphore.Weighted

func init() {
	if m, ok := os.LookupEnv("MAX_CONC_KERAS_CONV_PROCS"); !ok {
		sem = semaphore.NewWeighted(2) // default
	} else if n, err := strconv.Atoi(m); err != nil {
		sem = semaphore.NewWeighted(int64(n))
	} else {
		panic("MAX_CONC_KERAS_CONV_PROCS env var must have int value")
	}
}

func writeConfigPbtxt(filename string, modelConfig *triton.ModelConfig) error {
	var err error

	// for some level of human readability...
	marshalOpts := prototext.MarshalOptions{
		Multiline: true,
	}

	var pbtxtOut []byte
	if pbtxtOut, err = marshalOpts.Marshal(modelConfig); err != nil {
		return fmt.Errorf("Unable to marshal config.pbtxt: %w", err)
	}

	if err = os.WriteFile(filename, pbtxtOut, 0644); err != nil {
		return fmt.Errorf("Unable to write config.pbtxt: %w", err)
	}
	return nil
}

func convertKerasToTF(kerasFile string, targetPath string, ctx context.Context, loggr logr.Logger) error {
	cmd := exec.Command("python", "/opt/scripts/tf_pb.py", kerasFile, targetPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("Failed to create stdout pipe: %w ", err)
	}

	if err = sem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("Failed to acquire semaphore for keras conversion process: %w", err)
	}
	defer sem.Release(1)

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("Failed to start python process for keras model conversion: %w ", err)
	}
	go copyOutput(stdout, loggr)

	err = cmd.Wait()
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) != 0 {
		loggr.Error(err, "keras model conversion failed: %s", exitErr.Stderr)
		return fmt.Errorf("keras model conversion failed: %s: %w", exitErr.Stderr, err)
	} else if err != nil {
		loggr.Error(err, "keras model conversion failed")
		return fmt.Errorf("keras model conversion failed: %w", err)
	}
	return nil
}

func copyOutput(r io.Reader, loggr logr.Logger) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		loggr.Info(scanner.Text())
	}
}
