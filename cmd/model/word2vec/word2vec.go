// Copyright © 2020 wego authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package word2vec

import (
	"os"
	"path/filepath"
	"runtime/pprof"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/wujunfeng1/wego/cmd/model/cmdutil"
	"github.com/wujunfeng1/wego/pkg/model/modelutil/vector"
	"github.com/wujunfeng1/wego/pkg/model/word2vec"
)

var (
	prof       bool
	inputFile  string
	outputFile string
	vectorType vector.Type
)

func New() *cobra.Command {
	var opts word2vec.Options
	cmd := &cobra.Command{
		Use:   "word2vec",
		Short: "Word2Vec: Continuous Bag-of-Words and Skip-gram model",
		RunE: func(cmd *cobra.Command, args []string) error {
			return execute(opts)
		},
	}

	cmdutil.AddInputFlags(cmd, &inputFile)
	cmdutil.AddOutputFlags(cmd, &outputFile)
	cmdutil.AddProfFlags(cmd, &prof)
	cmdutil.AddVectorTypeFlags(cmd, &vectorType)
	word2vec.LoadForCmd(cmd, &opts)
	return cmd
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func execute(opts word2vec.Options) error {
	if prof {
		f, err := os.Create("cpu.prof")
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if fileExists(outputFile) {
		return errors.Errorf("%s is already existed", outputFile)
	} else if !fileExists(inputFile) {
		return errors.Errorf("%s is not found", inputFile)
	}
	if err := os.MkdirAll(filepath.Dir(outputFile), 0777); err != nil {
		return err
	}
	output, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	input, err := os.Open(inputFile)
	if err != nil {
		return err
	}
	defer input.Close()
	mod, err := word2vec.NewForOptions(opts)
	if err != nil {
		return err
	}
	if err := mod.Train(input); err != nil {
		return err
	}
	return mod.Save(output, vectorType)
}
