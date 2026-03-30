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

// Package cache provides a content addressable storage system that is safe to use concurrently.
// Acquire always returns a reader for consumers, and an optional writer if the resource does not exist yet,
// allowing read while downloading patterns.
// Cache size is configurable and has garbage collection mechanisms.
package cache
