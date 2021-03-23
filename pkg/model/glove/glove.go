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
	"context"
	"fmt"
	"io"
	"math/rand"
	"sync"

	"golang.org/x/sync/semaphore"

	"github.com/pkg/errors"
	"github.com/ynqa/wego/pkg/corpus"
	"github.com/ynqa/wego/pkg/corpus/fs"
	"github.com/ynqa/wego/pkg/corpus/memory"
	"github.com/ynqa/wego/pkg/model"
	"github.com/ynqa/wego/pkg/model/modelutil"
	"github.com/ynqa/wego/pkg/model/modelutil/matrix"
	"github.com/ynqa/wego/pkg/model/modelutil/vector"
	"github.com/ynqa/wego/pkg/util/clock"
	"github.com/ynqa/wego/pkg/util/verbose"
)

type glove struct {
	opts Options

	corpus corpus.Corpus

	param  *matrix.Matrix
	solver solver

	verbose *verbose.Verbose
}

func New(opts ...ModelOption) (model.Model, error) {
	options := DefaultOptions()
	for _, fn := range opts {
		fn(&options)
	}

	return NewForOptions(options)
}

func NewForOptions(opts Options) (model.Model, error) {
	// TODO: validate Options
	v := verbose.New(opts.Verbose)
	return &glove{
		opts: opts,

		verbose: v,
	}, nil
}

func (g *glove) Train(r io.ReadSeeker) error {
	if g.opts.DocInMemory {
		g.corpus = memory.New(r, g.opts.ToLower, g.opts.MaxCount, g.opts.MinCount)
	} else {
		g.corpus = fs.New(r, g.opts.ToLower, g.opts.MaxCount, g.opts.MinCount)
	}

	if err := g.corpus.Load(
		&corpus.WithCooccurrence{
			CountType: g.opts.CountType,
			Window:    g.opts.Window,
		},
		g.verbose, g.opts.LogBatch,
	); err != nil {
		return err
	}

	dic, dim := g.corpus.Dictionary(), g.opts.Dim

	dimAndBias := dim + 1
	g.param = matrix.New(
		dic.Len()*2,
		dimAndBias,
		func(_ int, vec []float64) {
			for i := 0; i < dim+1; i++ {
				vec[i] = rand.Float64() / float64(dim)
			}
		},
	)

	switch g.opts.SolverType {
	case Stochastic:
		g.solver = newStochastic(g.opts)
	case AdaGrad:
		g.solver = newAdaGrad(dic, g.opts)
	default:
		return errors.Errorf("invalid solver: %s not in %s|%s", g.opts.SolverType, Stochastic, AdaGrad)
	}

	return g.train()
}

func (g *glove) train() error {
	items := g.makeItems(g.corpus.Cooccurrence())
	itemSize := len(items)
	indexPerThread := modelutil.IndexPerThread(
		g.opts.Goroutines,
		itemSize,
	)

	for i := 0; i < g.opts.Iter; i++ {
		trained, clk := make(chan struct{}), clock.New()
		go g.observe(trained, clk)

		sem := semaphore.NewWeighted(int64(g.opts.Goroutines))
		wg := &sync.WaitGroup{}

		for i := 0; i < g.opts.Goroutines; i++ {
			wg.Add(1)
			s, e := indexPerThread[i], indexPerThread[i+1]
			go g.trainPerThread(items[s:e], trained, sem, wg)
		}

		wg.Wait()
		close(trained)
	}
	return nil
}

func (g *glove) trainPerThread(
	items []item,
	trained chan struct{},
	sem *semaphore.Weighted,
	wg *sync.WaitGroup,
) error {
	defer func() {
		wg.Done()
		sem.Release(1)
	}()

	if err := sem.Acquire(context.Background(), 1); err != nil {
		return err
	}

	dic := g.corpus.Dictionary()
	for _, item := range items {
		g.solver.trainOne(item.l1, item.l2+dic.Len(), g.param, item.f, item.coef)
		g.solver.trainOne(item.l1+dic.Len(), item.l2, g.param, item.f, item.coef)
		trained <- struct{}{}
	}

	return nil
}

func (g *glove) observe(trained chan struct{}, clk *clock.Clock) {
	var cnt int
	for range trained {
		g.verbose.Do(func() {
			cnt++
			if cnt%g.opts.LogBatch == 0 {
				fmt.Printf("trained %d items %v\r", cnt, clk.AllElapsed())
			}
		})
	}
	g.verbose.Do(func() {
		fmt.Printf("trained %d items %v\r\n", cnt, clk.AllElapsed())
	})
}

func (g *glove) Save(f io.Writer, typ vector.Type) error {
	return vector.Save(f, g.corpus.Dictionary(), g.WordVector(typ), g.verbose, g.opts.LogBatch)
}

func (g *glove) WordVector(typ vector.Type) *matrix.Matrix {
	var mat *matrix.Matrix
	dic := g.corpus.Dictionary()
	if typ == vector.Agg {
		mat = matrix.New(dic.Len(), g.opts.Dim,
			func(row int, vec []float64) {
				for i := 0; i < g.opts.Dim; i++ {
					vec[i] = g.param.Slice(row)[i]
				}
			},
		)
	} else {
		mat = matrix.New(dic.Len(), g.opts.Dim,
			func(row int, vec []float64) {
				for i := 0; i < g.opts.Dim; i++ {
					vec[i] = g.param.Slice(row)[i] + g.param.Slice(row + dic.Len())[i]
				}
			},
		)
	}
	return mat
}
