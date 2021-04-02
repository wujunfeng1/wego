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

package vector

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/wujunfeng1/wego/pkg/corpus/dictionary"
	"github.com/wujunfeng1/wego/pkg/model/modelutil/matrix"
	"github.com/wujunfeng1/wego/pkg/util/clock"
	"github.com/wujunfeng1/wego/pkg/util/verbose"
)

func InvalidTypeError(typ Type) error {
	return errors.Errorf("invalid vector type: %s not in %s|%s", typ, Single, Agg)
}

type Type = string

const (
	Single Type = "single"
	Agg    Type = "agg"
)

func Save(f io.Writer, dic *dictionary.Dictionary, mat *matrix.Matrix, verbose *verbose.Verbose, logBatch int) error {
	if dic.Len() != mat.Row() {
		return fmt.Errorf("different for length of dic and row of matrix: %d, %d", dic.Len(), mat.Row())
	}
	writer := bufio.NewWriter(f)
	defer writer.Flush()

	var buf bytes.Buffer
	clk := clock.New()
	for i := 0; i < dic.Len(); i++ {
		word, _ := dic.Word(i)
		fmt.Fprintf(&buf, "%v ", word)
		for j := 0; j < mat.Col(); j++ {
			fmt.Fprintf(&buf, "%f ", mat.Slice(i)[j])
		}
		fmt.Fprintln(&buf)
		verbose.Do(func() {
			if i%logBatch == 0 {
				fmt.Printf("saved %d words %v\r", i, clk.AllElapsed())
			}
		})
	}
	writer.WriteString(fmt.Sprintf("%v", buf.String()))
	verbose.Do(func() {
		fmt.Printf("saved %d words %v\r\n", dic.Len(), clk.AllElapsed())
	})
	return nil
}

func Load(f io.ReadSeeker, dic *dictionary.Dictionary, mat *matrix.Matrix, verbose *verbose.Verbose, logBatch int) error {
	if dic.Len() != mat.Row() {
		return fmt.Errorf("different for length of dic and row of matrix: %d, %d", dic.Len(), mat.Row())
	}
	scanner := bufio.NewScanner(f)

	clk := clock.New()
	numReads := 0
	for scanner.Scan() {
		textLine := scanner.Text()
		fields := strings.Split(textLine, " ")
		numFields := len(fields)
		if numFields < 1+mat.Col() {
			continue
		}
		word := fields[0]
		i, hasWord := dic.ID(word)
		if !hasWord {
			continue
		}
		row := mat.Slice(i)
		for j := 0; j < mat.Col(); j++ {
			value, err := strconv.ParseFloat(fields[j+1], 64)
			if err != nil {
				fmt.Println("error reading word vector: ", err)
			}
			row[j] = value
		}
		numReads++
		verbose.Do(func() {
			if numReads%logBatch == 0 {
				fmt.Printf("loaded %d words %v\r", i, clk.AllElapsed())
			}
		})
	}
	verbose.Do(func() {
		fmt.Printf("loaded %d words %v\r\n", numReads, clk.AllElapsed())
	})
	return nil
}
