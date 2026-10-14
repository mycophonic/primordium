/*
   Copyright Mycophonic.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

// Package refcount counts the holders of keyed directories across processes:
// an entry exists while any process holds it and goes with its last release.
//
// It is low level: the factory runs on every Acquire, so it decides itself
// what to do with an entry that is already filled, and only the last
// releaser's cleanup runs. For content written once and read by many, use
// the volatile store.
package refcount
