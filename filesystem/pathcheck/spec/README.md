# pathcheck formal spec

`pathcheck.qnt` defines, completely, which component names, paths and Unix
socket paths each platform accepts: Linux, Darwin and Windows. Each predicate
says exactly when a value is accepted, so it is an oracle in both directions.

`CONTRACT` states properties of those definitions, independent of any code:
dots are never names, Linux and Darwin agree, a valid name is a valid path,
valid names compose, a Windows long path behaves as the same path without its
prefix unless "/" is involved, a name valid on Windows is valid on Linux unless
it is longer than 255 bytes, device names are refused in any case, and
Darwin's socket limit is the stricter. Each is checked over every text up to a
bound, drawn from code points at each rule's edge.

Its state machine only generates test cases: each step picks a platform and a
text, half the time from tokens at every rule's edge and half the time on an
exact limit or a Windows separator rule, and records what pathcheck must
answer. `TestQuintTraces` replays every recorded state against the package.

pathcheck has no state, so there is no state machine to explore: the spec's
value is the formal contract, its checked properties, and the cases generated
from it. `quint verify` (Apalache) is not used: it is not part of the
toolchain, and the bounded properties are enumerated by the simulator.

## Run it

`--backend=typescript` matters: the default backend downloads an evaluator at
run time, which the toolchain does not allow.

```
quint typecheck filesystem/pathcheck/spec/pathcheck.qnt
quint run filesystem/pathcheck/spec/pathcheck.qnt --backend=typescript \
  --invariant=CONTRACT --max-samples=1 --max-steps=0
```

Regenerate the traces after changing the spec, and commit them with it:

```
rm -f filesystem/pathcheck/testdata/quint/*.itf.json.gz
quint run filesystem/pathcheck/spec/pathcheck.qnt --backend=typescript \
  --max-samples=30 --n-traces=30 --max-steps=9 --seed=7 \
  --out-itf='filesystem/pathcheck/testdata/quint/{seq}.itf.json'
gzip -9 -n filesystem/pathcheck/testdata/quint/*.itf.json
```
