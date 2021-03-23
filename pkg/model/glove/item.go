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

package glove

import (
	"fmt"
	"math"

	co "github.com/ynqa/wego/pkg/corpus/cooccurrence"
	"github.com/ynqa/wego/pkg/corpus/cooccurrence/encode"
	"github.com/ynqa/wego/pkg/util/clock"
)

type item struct {
	l1, l2 int
	f      float64
	coef   float64
}

func (g *glove) makeItems(cooc *co.Cooccurrence) []item {
	em := cooc.EncodedMatrix()
	res, idx, clk := make([]item, len(em)), 0, clock.New()
	for enc, f := range em {
		u1, u2 := encode.DecodeBigram(enc)
		l1, l2 := int(u1), int(u2)
		coef := 1.
		if f < float64(g.opts.Xmax) {
			coef = math.Pow(f/float64(g.opts.Xmax), g.opts.Alpha)
		}
		res[idx] = item{
			l1:   l1,
			l2:   l2,
			f:    math.Log(f),
			coef: coef,
		}
		idx++
		g.verbose.Do(func() {
			if idx%g.opts.LogBatch == 0 {
				fmt.Printf("build %d items %v\r", idx, clk.AllElapsed())
			}
		})
	}
	g.verbose.Do(func() {
		fmt.Printf("build %d items %v\r\n", idx, clk.AllElapsed())
	})
	return res
}
