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
	"github.com/ynqa/wego/pkg/model/modelutil/subsample"
	"github.com/ynqa/wego/pkg/model/modelutil/vector"
	"github.com/ynqa/wego/pkg/util/clock"
	"github.com/ynqa/wego/pkg/util/verbose"
)

type word2vec struct {
	opts Options

	corpus corpus.Corpus

	param      *matrix.Matrix
	subsampler *subsample.Subsampler
	currentlr  float64
	mod        mod
	optimizer  optimizer

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
	return &word2vec{
		opts: opts,

		currentlr: opts.Initlr,

		verbose: v,
	}, nil
}

func (w *word2vec) Train(r io.ReadSeeker) error {
	if w.opts.DocInMemory {
		w.corpus = memory.New(r, w.opts.ToLower, w.opts.MaxCount, w.opts.MinCount)
	} else {
		w.corpus = fs.New(r, w.opts.ToLower, w.opts.MaxCount, w.opts.MinCount)
	}

	if err := w.corpus.Load(nil, w.verbose, w.opts.LogBatch); err != nil {
		return err
	}

	dic, dim := w.corpus.Dictionary(), w.opts.Dim

	w.param = matrix.New(
		dic.Len(),
		dim,
		func(_ int, vec []float64) {
			for i := 0; i < dim; i++ {
				vec[i] = (rand.Float64() - 0.5) / float64(dim)
			}
		},
	)

	w.subsampler = subsample.New(dic, w.opts.SubsampleThreshold)

	switch w.opts.ModelType {
	case SkipGram:
		w.mod = newSkipGram(w.opts)
	case Cbow:
		w.mod = newCbow(w.opts)
	default:
		return errors.Errorf("invalid model: %s not in %s|%s", w.opts.ModelType, Cbow, SkipGram)
	}

	switch w.opts.OptimizerType {
	case NegativeSampling:
		w.optimizer = newNegativeSampling(
			w.corpus.Dictionary(),
			w.opts,
		)
	case HierarchicalSoftmax:
		w.optimizer = newHierarchicalSoftmax(
			w.corpus.Dictionary(),
			w.opts,
		)
	default:
		return errors.Errorf("invalid optimizer: %s not in %s|%s", w.opts.OptimizerType, NegativeSampling, HierarchicalSoftmax)
	}

	if w.opts.DocInMemory {
		if err := w.train(); err != nil {
			return err
		}
	} else {
		if err := w.batchTrain(); err != nil {
			return err
		}
	}
	return nil
}

func (w *word2vec) train() error {
	doc := w.corpus.IndexedDoc()
	indexPerThread := modelutil.IndexPerThread(
		w.opts.Goroutines,
		len(doc),
	)

	for i := 1; i <= w.opts.Iter; i++ {
		fmt.Printf("train iter %d\n", i)
		trained, clk := make(chan int), clock.New()
		go w.observe(trained, clk)

		sem := semaphore.NewWeighted(int64(w.opts.Goroutines))
		wg := &sync.WaitGroup{}

		for i := 0; i < w.opts.Goroutines; i++ {
			wg.Add(1)
			s, e := indexPerThread[i], indexPerThread[i+1]
			go w.trainPerThread(doc[s:e], trained, sem, wg)
		}

		wg.Wait()
		close(trained)
	}
	return nil
}

func (w *word2vec) batchTrain() error {
	for i := 1; i <= w.opts.Iter; i++ {
		trained, clk := make(chan int), clock.New()
		go w.observe(trained, clk)

		sem := semaphore.NewWeighted(int64(w.opts.Goroutines))
		wg := &sync.WaitGroup{}

		in := make(chan []int, w.opts.Goroutines)
		go w.corpus.BatchWords(in, w.opts.BatchSize)
		for doc := range in {
			wg.Add(1)
			go w.trainPerThread(doc, trained, sem, wg)
		}

		wg.Wait()
		close(trained)
	}
	return nil
}

func (w *word2vec) trainPerThread(
	doc []int,
	trained chan int,
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

	numTrain := 0
	for pos, id := range doc {
		if w.subsampler.Trial(id) {
			w.mod.trainOne(doc, pos, w.currentlr, w.param, w.optimizer)
		}
		numTrain++

		reportFreq := w.opts.LogBatch
		if reportFreq <= 0 {
			reportFreq = 1000
		}
		if numTrain%reportFreq == 0 {
			trained <- numTrain
			numTrain = 0
		}
	}
	if numTrain > 0 {
		trained <- numTrain
	}

	return nil
}

func (w *word2vec) observe(trained chan int, clk *clock.Clock) {
	var cnt int
	for numTrained := range trained {
		cnt += numTrained
		if cnt%w.opts.UpdateLRBatch == 0 {
			if w.currentlr < w.opts.MinLR {
				w.currentlr = w.opts.MinLR
			} else {
				w.currentlr = w.opts.Initlr * (1.0 - float64(cnt)/float64(w.corpus.Len()))
			}
		}
		w.verbose.Do(func() {
			if cnt%w.opts.LogBatch == 0 {
				fmt.Printf("trained %d words %v\r", cnt, clk.AllElapsed())
			}
		})
	}
	w.verbose.Do(func() {
		fmt.Printf("trained %d words %v\r\n", cnt, clk.AllElapsed())
	})
}

func (w *word2vec) Save(f io.Writer, typ vector.Type) error {
	return vector.Save(f, w.corpus.Dictionary(), w.WordVector(typ), w.verbose, w.opts.LogBatch)
}

func (w *word2vec) WordVector(typ vector.Type) *matrix.Matrix {
	var mat *matrix.Matrix
	dic := w.corpus.Dictionary()
	ng, ok := w.optimizer.(*negativeSampling)
	if typ == vector.Agg && ok {
		mat = matrix.New(dic.Len(), w.opts.Dim,
			func(row int, vec []float64) {
				for i := 0; i < w.opts.Dim; i++ {
					vec[i] = w.param.Slice(row)[i] + ng.ctx.Slice(row)[i]
				}
			},
		)
	} else {
		mat = matrix.New(dic.Len(), w.opts.Dim,
			func(row int, vec []float64) {
				for i := 0; i < w.opts.Dim; i++ {
					vec[i] = w.param.Slice(row)[i]
				}
			},
		)
	}
	return mat
}
