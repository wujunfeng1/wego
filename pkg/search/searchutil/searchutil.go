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

package searchutil

func Cosine(v1, v2 []float64, n1, n2 float64) float64 {
	if n1 == 0 || n2 == 0 {
		return 0
	}
	var dot float64
	for i := range v1 {
		dot += v1[i] * v2[i]
	}
	return dot / n1 / n2
}
