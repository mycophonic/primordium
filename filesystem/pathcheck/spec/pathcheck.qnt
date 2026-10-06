// pathcheck: which paths, names and socket paths each platform accepts.
//
// A complete definition, not a list of checks: every predicate says exactly
// when a value is accepted, so a rule that refuses too much is as wrong as one
// that accepts too much. Text is a list of Unicode code points: Quint strings
// support only equality. Text that is not valid UTF-8 is outside the model.
//
// The state machine below exists only to generate test cases: each step picks a
// platform and a text, and records what pathcheck must answer for it.
//
// Check:  quint typecheck filesystem/pathcheck/spec/pathcheck.qnt
// Traces: see filesystem/pathcheck/spec/README.md
module pathcheck {
  type Platform = Linux | Darwin | Windows
  type Text = List[int]

  pure val NUL = 0
  pure val SPACE = 32
  pure val QUOTE = 34
  pure val STAR = 42
  pure val DOT = 46
  pure val SLASH = 47
  pure val COLON = 58
  pure val LESS = 60
  pure val GREATER = 62
  pure val QUESTION = 63
  pure val BACKSLASH = 92
  pure val PIPE = 124

  // ---------------------------------------------------------------------------
  // Text helpers.

  pure def utf8Len(c: int): int =
    if (c < 128) 1 else if (c < 2048) 2 else if (c < 65536) 3 else 4

  pure def utf16Len(c: int): int = if (c < 65536) 1 else 2

  pure def bytes(t: Text): int = t.foldl(0, (n, c) => n + utf8Len(c))

  pure def units(t: Text): int = t.foldl(0, (n, c) => n + utf16Len(c))

  pure def has(t: Text, cs: Set[int]): bool = t.foldl(false, (found, c) => found or cs.contains(c))

  pure def startsWith(t: Text, prefix: Text): bool =
    if (t.length() < prefix.length()) false else t.slice(0, prefix.length()) == prefix

  pure def drop(t: Text, n: int): Text = t.slice(n, t.length())

  pure def lastIs(t: Text, cs: Set[int]): bool =
    if (t.length() == 0) false else cs.contains(t.nth(t.length() - 1))

  pure def isLetter(c: int): bool = (c >= 65 and c <= 90) or (c >= 97 and c <= 122)

  // Case folding for the names compared case-insensitively here, which are
  // ASCII letters and digits: no other code point folds to one of them.
  pure def lower(c: int): int = if (c >= 65 and c <= 90) c + 32 else c

  pure def lowerText(t: Text): Text = t.foldl(List(), (out, c) => out.append(lower(c)))

  // What precedes the first dot.
  pure def beforeDot(t: Text): Text =
    t.foldl({ out: List(), done: false }, (acc, c) =>
      if (acc.done or c == DOT) { out: acc.out, done: true }
      else { out: acc.out.append(c), done: false }
    ).out

  // Unicode white space, as strings.TrimSpace removes it.
  pure val WHITESPACE: Set[int] =
    Set(9, 10, 11, 12, 13, 32, 133, 160, 5760, 8232, 8233, 8239, 8287, 12288)
      .union(8192.to(8202))

  pure def blank(t: Text): bool = t.foldl(true, (spaces, c) => spaces and WHITESPACE.contains(c))

  // ---------------------------------------------------------------------------
  // Components.

  pure val COMPONENT_MAX = 255

  // A component's length as the platform counts it: UTF-16 code units on
  // Windows, bytes elsewhere.
  pure def nameLength(p: Platform, t: Text): int = match p {
    | Windows => units(t)
    | _ => bytes(t)
  }

  pure val POSIX_RESERVED: Set[int] = Set(NUL, SLASH)

  pure val WINDOWS_RESERVED: Set[int] =
    0.to(31).union(Set(LESS, GREATER, COLON, QUOTE, SLASH, BACKSLASH, PIPE, QUESTION, STAR))

  // con, prn, aux, nul; com and lpt followed by 1-9 or a superscript 1-3.
  pure val DEVICE_DIGITS: Set[int] = 49.to(57).union(Set(185, 178, 179))
  pure val DEVICES: Set[Text] =
    Set(List(99, 111, 110), List(112, 114, 110), List(97, 117, 120), List(110, 117, 108))
      .union(DEVICE_DIGITS.map(d => List(99, 111, 109, d)))
      .union(DEVICE_DIGITS.map(d => List(108, 112, 116, d)))

  // A device name, alone or before an extension, in any case.
  pure def device(t: Text): bool = DEVICES.contains(lowerText(beforeDot(t)))

  pure def validComponent(p: Platform, name: Text): bool = and {
    nameLength(p, name) <= COMPONENT_MAX,
    not(blank(name)),
    match p {
      | Windows => and {
          not(has(name, WINDOWS_RESERVED)),
          not(device(name)),
          not(lastIs(name, Set(DOT, SPACE))),
        }
      | _ => and {
          not(has(name, POSIX_RESERVED)),
          name != List(DOT),
          name != List(DOT, DOT),
        }
    },
  }

  // ---------------------------------------------------------------------------
  // Paths.

  pure val LONG_PREFIX: Text = List(BACKSLASH, BACKSLASH, QUESTION, BACKSLASH)
  pure val UNC: Text = List(117, 110, 99, BACKSLASH)

  pure def hasDrive(t: Text): bool =
    if (t.length() < 2) false else t.nth(1) == COLON and isLetter(t.nth(0))

  // "/" separates except on Windows in a long path, where only "\" does.
  pure def separates(p: Platform, long: bool, c: int): bool = match p {
    | Windows => c == BACKSLASH or (c == SLASH and not(long))
    | _ => c == SLASH
  }

  // The non-empty runs between separators, as strings.FieldsFunc splits.
  pure def pieces(p: Platform, long: bool, t: Text): List[Text] = {
    val acc = t.foldl({ done: List(), part: List() }, (acc, c) =>
      if (separates(p, long, c))
        { done: if (acc.part.length() > 0) acc.done.append(acc.part) else acc.done, part: List() }
      else
        { done: acc.done, part: acc.part.append(c) }
    )
    if (acc.part.length() > 0) acc.done.append(acc.part) else acc.done
  }

  // A path's components. On Windows a drive letter is not one, and neither is
  // a long path's prefix before a drive (`\\?\C:`) or a share (`\\?\UNC\`).
  // Any other path keeps every character, so a device path is refused by its
  // "?" or "." component.
  pure def components(p: Platform, path: Text): List[Text] = match p {
    | Windows =>
        if (startsWith(path, LONG_PREFIX)) {
          val rest = drop(path, LONG_PREFIX.length())
          if (startsWith(lowerText(rest), UNC)) pieces(Windows, true, drop(rest, UNC.length()))
          else if (hasDrive(rest)) pieces(Windows, true, drop(rest, 2))
          else pieces(Windows, false, path)
        }
        else if (hasDrive(path)) pieces(Windows, false, drop(path, 2))
        else pieces(Windows, false, path)
    | _ => pieces(p, false, path)
  }

  pure def validPath(p: Platform, path: Text): bool =
    components(p, path).foldl(true, (ok, name) => ok and validComponent(p, name))

  // ---------------------------------------------------------------------------
  // Unix socket paths: sockaddr_un's sun_path, NUL terminator included, holds
  // 108 bytes on Linux and Windows and 104 on Darwin.

  pure def socketMax(p: Platform): int = match p {
    | Darwin => 104
    | _ => 108
  }

  pure def validSocket(p: Platform, path: Text): bool = bytes(path) <= socketMax(p) - 1

  // ---------------------------------------------------------------------------
  // Properties of the contract itself, independent of any implementation, each
  // checked over every text up to a bound: CONTRACT is checked as an invariant
  // (see README). A failure is a contradiction or a gap in the rules above.

  pure val PLATFORMS: Set[Platform] = Set(Linux, Darwin, Windows)

  // One code point at each rule's edge; SHORT is every text of up to three of
  // them, PAIRED every text of up to two.
  pure val ALPHABET: Set[int] = Set(97, DOT, SPACE, SLASH, BACKSLASH, COLON, QUESTION, NUL, 233, 128512)
  pure val SHORT: Set[Text] = ALPHABET.allListsUpTo(3)
  pure val PAIRED: Set[Text] = ALPHABET.allListsUpTo(2)

  pure def upper(t: Text): Text = t.foldl(List(), (out, c) => out.append(if (c >= 97 and c <= 122) c - 32 else c))

  // "." and ".." are never names.
  pure val dotsAreNotNames: bool =
    PLATFORMS.forall(p => not(validComponent(p, List(DOT))) and not(validComponent(p, List(DOT, DOT))))

  // Linux and Darwin share their name and path rules.
  pure val posixPlatformsAgree: bool = SHORT.forall(t => and {
    validComponent(Linux, t) == validComponent(Darwin, t),
    validPath(Linux, t) == validPath(Darwin, t),
  })

  // A valid name is a valid path, made of itself alone.
  pure val aNameIsAPath: bool = PLATFORMS.forall(p => SHORT.forall(t =>
    validComponent(p, t) implies (validPath(p, t) and components(p, t) == List(t))
  ))

  // Two valid names joined by a separator are a valid path.
  pure val namesCompose: bool = PLATFORMS.forall(p => PAIRED.forall(a => PAIRED.forall(b =>
    (validComponent(p, a) and validComponent(p, b)) implies validPath(p, a.append(SLASH).concat(b))
  )))

  // On Windows, a long path is as valid as the same path without its prefix,
  // as long as no "/" is involved: only "/" behaves differently there.
  pure val longPathsMatchShortOnes: bool = PAIRED.forall(a => PAIRED.forall(b => {
    val rest = List(67, COLON, BACKSLASH).concat(a).append(BACKSLASH).concat(b)
    has(rest, Set(SLASH)) or validPath(Windows, LONG_PREFIX.concat(rest)) == validPath(Windows, rest)
  }))

  // A name valid on Windows is valid on Linux too, unless it is longer than
  // 255 bytes: Windows counts UTF-16 code units, so 200 "é" pass there and are
  // 400 bytes on Linux.
  pure val windowsNamesArePortable: bool = SHORT.forall(t =>
    (validComponent(Windows, t) and bytes(t) <= COMPONENT_MAX) implies validComponent(Linux, t)
  )
  pure val windowsByteLimitDiffers: bool = and {
    validComponent(Windows, repeat(233, 200)),
    not(validComponent(Linux, repeat(233, 200))),
  }

  // Device names are refused in any case, alone or before an extension.
  pure val devicesAreRefused: bool = DEVICES.forall(d => and {
    not(validComponent(Windows, d)),
    not(validComponent(Windows, upper(d))),
    not(validComponent(Windows, upper(d).concat(List(DOT, 116, 120, 116)))),
    validComponent(Windows, d.concat(List(120))),
  })

  // Darwin's socket limit is the stricter: over texts at and around each
  // limit, what Darwin accepts every platform accepts, and something Linux
  // accepts Darwin refuses.
  pure val SOCKET_EDGES: Set[Text] = Set(103, 104, 107, 108).map(n => repeat(120, n))
  pure val darwinSocketsAreStricter: bool = and {
    SOCKET_EDGES.forall(t => validSocket(Darwin, t) implies PLATFORMS.forall(p => validSocket(p, t))),
    SOCKET_EDGES.exists(t => validSocket(Linux, t) and not(validSocket(Darwin, t))),
  }

  val CONTRACT: bool = and {
    dotsAreNotNames,
    posixPlatformsAgree,
    aNameIsAPath,
    namesCompose,
    longPathsMatchShortOnes,
    windowsNamesArePortable,
    windowsByteLimitDiffers,
    devicesAreRefused,
    darwinSocketsAreStricter,
  }

  // ---------------------------------------------------------------------------
  // Test-case generation. Half the texts are built from tokens at every rule's
  // edge; the other half sit on an exact length limit.

  pure def repeat(c: int, n: int): Text = 0.to(n - 1).fold(List(), (out, _) => out.append(c))

  pure val TOKENS: Set[Text] = Set(
    List(),
    List(97), List(66), List(67, COLON), List(DOT), List(DOT, DOT), List(SPACE),
    List(SLASH), List(BACKSLASH), List(COLON), List(QUESTION), List(STAR), List(LESS),
    List(NUL), List(31), List(9), List(160),
    List(233), List(128512), List(185), List(57), List(48),
    List(99, 111, 110), List(78, 85, 76), List(99, 111, 109), List(108, 112, 116),
    LONG_PREFIX, List(85, 78, 67, BACKSLASH), List(BACKSLASH, BACKSLASH, DOT, BACKSLASH),
    repeat(97, 100), repeat(233, 60), repeat(128512, 30),
  )

  // Texts on an edge a random mix rarely lands on: an exact limit (254, 255
  // and 256 bytes or UTF-16 code units, each platform's last socket byte and
  // the one past it; "é" is 2 bytes and 1 unit, "😀" 4 bytes and 2 units);
  // "/" between two names in an ordinary and in a long Windows path, and the
  // names a rule refuses by itself: dots and device names.
  pure val EDGES: Set[Text] = Set(
    List(DOT), List(DOT, DOT), List(DOT, DOT, DOT),
    List(99, 111, 109, 185), List(108, 112, 116, 178), List(67, 79, 78, DOT, 116, 120, 116), List(99, 111, 109, 48),
    List(67, COLON, SLASH, 97, BACKSLASH, 98),
    LONG_PREFIX.concat(List(67, COLON, BACKSLASH, 97, SLASH, 98)),
    LONG_PREFIX.concat(List(85, 78, 67, BACKSLASH, 115, BACKSLASH, 104, SLASH, 120)),
    LONG_PREFIX.concat(List(85, 78, 67, BACKSLASH, 115, BACKSLASH, 104, BACKSLASH, 120)),
    repeat(97, 254), repeat(97, 255), repeat(97, 256),
    repeat(233, 127), repeat(233, 128), repeat(233, 255), repeat(233, 256),
    repeat(128512, 63), repeat(128512, 64), repeat(128512, 127), repeat(128512, 128),
    repeat(120, 103), repeat(120, 104), repeat(120, 107), repeat(120, 108),
  )

  var platform: Platform
  var text: Text
  var component: bool
  var path: bool
  var socket: bool

  action pick = {
    nondet p = PLATFORMS.oneOf()
    nondet a = TOKENS.oneOf()
    nondet b = TOKENS.oneOf()
    nondet c = TOKENS.oneOf()
    nondet d = TOKENS.oneOf()
    nondet e = TOKENS.oneOf()
    nondet edge = EDGES.oneOf()
    nondet onEdge = Set(true, false).oneOf()
    val t = if (onEdge) edge else a.concat(b).concat(c).concat(d).concat(e)
    all {
      platform' = p,
      text' = t,
      component' = validComponent(p, t),
      path' = validPath(p, t),
      socket' = validSocket(p, t),
    }
  }

  action init = pick
  action step = pick
}
