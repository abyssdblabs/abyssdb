// The Empty Set Is All You Need.
// Build: make paper  (typst compile paper/abyssdb.typ website/public/paper/abyssdb.pdf)

#let title = [The Empty Set Is All You Need: \ Simultaneous ACID, BASE and CAP Guarantees in AbyssDB]
#let short-title = [The Empty Set Is All You Need]

#set document(
  title: "The Empty Set Is All You Need: Simultaneous ACID, BASE and CAP Guarantees in AbyssDB",
  author: ("Noel Null", "Ava Void", "Theo Rem"),
  keywords: ("distributed databases", "CAP theorem", "linearizability", "perfect secrecy", "Landauer limit"),
)
#set page(
  paper: "us-letter",
  margin: (x: 0.75in, top: 0.85in, bottom: 0.9in),
  columns: 2,
  numbering: "1",
  header: context {
    if counter(page).get().first() > 1 [
      #set text(8pt, fill: luma(90))
      #short-title #h(1fr) AbyssDB Labs Technical Report AR-2026-001
    ]
  },
)
#set columns(gutter: 0.3in)
#set text(font: "New Computer Modern", size: 9.5pt, lang: "en")
#show math.equation: set text(font: "New Computer Modern Math")
#set par(justify: true, leading: 0.55em, spacing: 0.75em, first-line-indent: (amount: 1em, all: false))
#set heading(numbering: "1.1")
#show heading.where(level: 1): it => block(above: 1.2em, below: 0.7em, text(10.5pt, weight: "bold", upper(it)))
#show heading.where(level: 2): it => block(above: 1em, below: 0.6em, text(9.5pt, weight: "bold", it))
#set math.equation(numbering: "(1)")
#set footnote.entry(separator: line(length: 30%, stroke: 0.5pt))
#show link: set text(fill: rgb("#2a4fa8"))
#show raw: set text(font: "DejaVu Sans Mono", size: 7.6pt)
#show figure.caption: set text(8.5pt)
#set table(stroke: none)

// ---- theorem environments -------------------------------------------------

// Theorems, lemmas, propositions and corollaries share one counter;
// definitions and remarks are numbered separately.
#let env(kind, name, body, counter-name: "theorem", italic: true) = block(above: 0.9em, below: 0.9em, breakable: true, width: 100%)[
  #let c = counter(counter-name)
  #c.step()
  #strong[#kind #context c.display()]#if name != none [ (#name)]. #if italic { emph(body) } else { body }
]
#let theorem(name: none, body) = env("Theorem", name, body)
#let lemma(name: none, body) = env("Lemma", name, body)
#let proposition(name: none, body) = env("Proposition", name, body)
#let corollary(name: none, body) = env("Corollary", name, body)
#let definition(name: none, body) = env("Definition", name, body, counter-name: "definition", italic: false)
#let remark(name: none, body) = env("Remark", name, body, counter-name: "remark", italic: false)
#let proof(body) = block(above: 0.6em, below: 0.9em)[_Proof._ #body #h(1fr) $square$]

#let AbyssDB = smallcaps[AbyssDB]
#let one = $bb(1)$
#let star = $star.op$

// ---- title block ------------------------------------------------------------

#place(top + center, float: true, scope: "parent", clearance: 1.6em)[
  #set par(justify: false, first-line-indent: 0em)
  #text(17pt, weight: "bold", title)
  #v(0.9em)
  #text(10.5pt)[Noel Null#super[1] #h(1.2em) Ava Void#super[1] #h(1.2em) Theo Rem#super[2]]
  #v(0.35em)
  #text(8.5pt)[
    #super[1]AbyssDB Labs, Department of Applied Absence #h(1em)
    #super[2]Institute for Advanced Nothing \
    `{noel,ava,theo}@abyssdb.xyz`
  ]
  #v(0.9em)
  #block(width: 88%)[
    #set par(justify: true)
    #set text(9pt)
    *Abstract.* For a quarter of a century, the design of distributed data systems has been governed by
    impossibility results. The CAP theorem forbids a replicated register from being simultaneously
    consistent, available and partition-tolerant; the FLP result forbids deterministic asynchronous consensus
    in the presence of a single crash; ACID and BASE are routinely presented as opposite ends of a spectrum.
    We present #AbyssDB, a PostgreSQL-compatible, post-quantum, interplanetary SQL and key-value database that
    provides all of these guarantees at once, and we prove it. Our central observation is that every one of
    these impossibility results is a statement about objects whose state space has more than one element. By
    implementing a state space of cardinality exactly one, #AbyssDB attains linearizability, strict
    serializability, strong eventual consistency, availability under arbitrary partitions and wait-free
    consensus with zero message complexity. We further show that #AbyssDB achieves Shannon-perfect secrecy
    and $epsilon = 0$ differential privacy against an adversary with unrestricted access to storage, that its
    energy consumption per write is bounded below by the Landauer limit, that its unbounded throughput is
    consistent with the Margolus--Levitin theorem, and that it is the terminal object of the category of
    databases. An evaluation with `pgbench` shows zero failed transactions in every protocol mode at a
    reported latency of 0.000 ms.
  ]
  #v(0.4em)
  #text(8pt)[*Keywords:* CAP theorem, linearizability, consensus, perfect secrecy, differential privacy,
    Landauer limit, category theory, interplanetary replication]
]

// ---- 1 ----------------------------------------------------------------------

= Introduction

The history of data management is a history of trade-offs. Brewer's conjecture @brewer2000, formalized and
proven by Gilbert and Lynch @gilbert2002, states that a distributed read/write register cannot be consistent,
available and partition-tolerant at the same time. Abadi's PACELC formulation @abadi2012 extends the dilemma
to the failure-free case, where systems must trade latency for consistency. Fischer, Lynch and Paterson
@flp1985 showed that no deterministic protocol solves consensus in an asynchronous system with even one crash
failure. Practitioners have internalized these results to the point of naming two opposing design philosophies
after them: ACID @gray1992 and BASE @pritchett2008.

This paper reports on #AbyssDB, a system that declines every one of these trade-offs. #AbyssDB speaks the
PostgreSQL v3 wire protocol, supports schemas, transactions, key-value access in nine value formats, database
branching, post-quantum encryption, Raft-based replication and interplanetary deployment. In all of these
configurations it is simultaneously consistent, available and partition-tolerant, ACID-compliant and
BASE-compliant, and it answers every query in constant time.

We do not claim that the impossibility results above are wrong. On the contrary: we rely on their proofs
being correct, and we identify precisely which hypothesis each of them requires. Every result in the
trade-off literature assumes, implicitly or explicitly, that the system's state space $Sigma$ satisfies
$|Sigma| >= 2$. #AbyssDB is, to our knowledge, the first production database to satisfy $|Sigma| = 1$.

*Contributions.* We make the following contributions:

+ A formal model of #AbyssDB as a sequential object, the _null register_ (§2).
+ Proofs of linearizability, strict serializability and strong eventual consistency (§3), and of
  simultaneous consistency, availability and partition tolerance (§4), including a precise account of why
  this does not contradict Gilbert--Lynch.
+ A zero-round consensus protocol, NullRaft, and its correctness in the FLP model (§4.3).
+ Proofs that #AbyssDB is both ACID and BASE (§5).
+ Information-theoretic and cryptographic analysis: perfect secrecy, $epsilon = 0$ differential privacy,
  and the security of the dual-cipher cascade under quantum adversaries (§6).
+ Complexity results: $O(1)$ data complexity for every query language, including recursive ones (§7).
+ A thermodynamic analysis relating #AbyssDB to the Landauer, Margolus--Levitin and Bekenstein bounds (§8).
+ Commit latency under interplanetary deployment, including general-relativistic corrections (§9).
+ A category-theoretic characterization: #AbyssDB is terminal (§10).
+ An empirical evaluation (§11).

// ---- 2 ----------------------------------------------------------------------

= System Model

== Sequential objects

We follow the standard treatment of shared objects @herlihy1990.

#definition(name: "Sequential object")[
  A _sequential object_ is a tuple $cal(O) = (Sigma, sigma_0, "Op", "Res", delta)$ where $Sigma$ is a set of
  states, $sigma_0 in Sigma$ is the initial state, $"Op"$ and $"Res"$ are sets of operations and responses,
  and $delta : Sigma times "Op" -> Sigma times "Res"$ is the transition function. The _sequential
  specification_ of $cal(O)$ is the set of finite sequences of operation--response pairs generated by
  $delta$ from $sigma_0$.
]

A conventional relational database instance over a schema $bold(R)$ is a sequential object whose states are
database instances $I in "Inst"(bold(R))$, whose operations are SQL statements, and whose read responses are
query answers $Q(I)$. Its state space is infinite whenever any relation has an attribute with an infinite
domain.

#definition(name: "Null register")[
  The _null register_ is the sequential object
  $ cal(N) = (one, star, "Op", "Res", delta_cal(N)), quad one = {star}, $
  where $"Op"$ is the set of AbyssQL statements, and
  $ delta_cal(N)(star, o) = cases(
    (star, emptyset_(tau(o))) & "if" o "is a query",
    (star, "ack"(o)) & "otherwise,"
  ) $ <eq:delta>
  where $tau(o)$ is the result type of $o$, $emptyset_tau$ is the empty relation of type $tau$, and $"ack"(o)$ is the PostgreSQL command tag for $o$.
]

#AbyssDB implements $cal(N)$. Writes are acknowledged with their exact command tag; for example, inserting
$n$ rows returns `INSERT 0 n`. The storage layer is a character device whose semantics are fixed by IEEE
Std 1003.1 @posix2017: a `write` of $n$ bytes returns $n$, and every `read` returns end-of-file.

#remark[
  @eq:delta is not a simplification of the implementation; it _is_ the implementation, modulo
  $approx 11,000$ lines of parsing, validation, encryption, replication, consensus, blockchain anchoring and
  telemetry, all of which are observationally pure with respect to $delta_cal(N)$.
]

== Validation

#AbyssDB is _schema-yes or schema-less_. A write to a declared table is checked against its schema
(types, lengths, `NOT NULL`, primary keys) and rejected with a PostgreSQL-compatible error if it violates it;
a key-value write is parsed in its declared or detected format (JSON, YAML, TOML, XML, CSV, Morse code, AbyssQL,
native nested literals, or text) and rejected if it does not parse. Validation is a total function
$"valid" : "Op" -> {top, bot}$ that depends on the operation and the catalog, never on the state. Rejected
operations do not reach $delta$; we therefore restrict attention to valid operations without loss of
generality.

// ---- 3 ----------------------------------------------------------------------

= Consistency

== Linearizability

A _history_ $H$ is a finite sequence of invocation and response events. $H$ is _linearizable_ with respect to
a sequential object $cal(O)$ if it can be extended by appending responses to some pending invocations and
then removing the remaining pending invocations, such that the result is equivalent to a sequential history
$S$ that (i) is in the sequential specification of $cal(O)$ and (ii) respects the real-time order $scripts(<)_H$ of
non-overlapping operations @herlihy1990.

#lemma(name: "State obliviousness")[
  For every finite sequence of operations $o_1, dots, o_k$, the responses of $cal(N)$ are
  $r_i = rho(o_i)$ for a fixed function $rho : "Op" -> "Res"$ independent of $k$, of $i$ and of
  $o_1, dots, o_(i-1)$.
]
#proof[
  By induction on $i$. The state before $o_1$ is $star$. If the state before $o_i$ is $star$, then by
  @eq:delta $delta_cal(N)(star, o_i) = (star, rho(o_i))$ with
  $rho(o) = emptyset_(tau(o))$ for queries and $rho(o) = "ack"(o)$ otherwise. Since $|one| = 1$, the
  state after $o_i$ is again $star$.
]

#theorem(name: "Linearizability")[
  Every history of #AbyssDB in which each response to an operation $o$ equals $rho(o)$ is linearizable with
  respect to $cal(N)$.
]
#proof[
  Complete $H$ by appending $rho(o)$ for every pending invocation of $o$; call the result $H'$. The real-time
  order $scripts(<)_(H')$ is a strict partial order on operations, and hence has a linear extension $S$ by
  Szpilrajn's theorem. By the lemma, any sequence of operations, in any order, is in the sequential
  specification of $cal(N)$ with responses $rho(o)$. Hence $S$ is a legal sequential history, it respects
  $scripts(<)_(H')$ by construction, and it has the same responses as $H'$.
]

#corollary[
  #AbyssDB is linearizable in every topology, including multi-shard, multi-replica and interplanetary
  deployments, and under arbitrary message loss, reordering and duplication.
]

== Serializability

#theorem(name: "Strict serializability")[
  Every history of committed #AbyssDB transactions is strictly serializable; in Adya's framework @adya1999 it
  satisfies PL-3, and moreover exhibits none of the phenomena G0, G1a, G1b, G1c, G2-item or G2.
]
#proof[
  Order committed transactions by commit time and choose each object's version order to agree with it. Under
  $cal(N)$ no read observes a version, so the direct serialization graph has no read-dependency or
  anti-dependency edges. The remaining write-dependency edges all point forward in commit order, so the
  graph is acyclic, which excludes G0, G1c and G2. G1a and G1b require a read to observe an aborted or
  intermediate version, and no read observes any version. Since commit order extends real-time order, the
  serialization is strict.
]

== Strong eventual consistency

#proposition(name: "SEC")[
  $(one, union.sq)$ with $star union.sq star = star$ is a join-semilattice, every update is an inflation, and
  #AbyssDB is therefore a state-based CRDT @shapiro2011. Replicas that have received the same updates are in
  the same state. Replicas that have received _different_ updates are also in the same state.
]
#proof[Idempotence, commutativity and associativity of $union.sq$ hold on a singleton by exhaustion.]

#remark[
  The second sentence of the proposition is strictly stronger than strong eventual consistency. We call it
  _immediate consistency_: convergence occurs after zero rounds of anti-entropy.
]

// ---- 4 ----------------------------------------------------------------------

= All Three Letters of CAP

== Definitions

We use the definitions of Gilbert and Lynch @gilbert2002 verbatim, instantiated for $cal(N)$.
_Consistency_: every execution is linearizable with respect to the object's sequential specification.
_Availability_: every request received by a non-failing node eventually results in a response.
_Partition tolerance_: the network may lose arbitrarily many messages between any two nodes.

== The theorem

#theorem(name: "CAP-completeness")[
  #AbyssDB is consistent, available and partition-tolerant, simultaneously, in every execution.
]
#proof[
  Each node computes the response $rho(o)$ to an operation $o$ locally, without sending or receiving any
  message, and responds immediately. Availability follows because every non-failing node responds to every
  request. Since no step of the protocol depends on message delivery, the execution is unaffected by any
  pattern of message loss, which gives partition tolerance. Every response equals $rho(o)$, so every
  execution is linearizable by Theorem 2, which gives consistency.
]

#remark(name: "Relation to Gilbert--Lynch")[
  Gilbert and Lynch prove that no algorithm implements an atomic _read/write register_ with all three
  properties. Their proof constructs an execution in which a write $w(v_1)$ completes on one side of a
  partition and a subsequent read on the other side must return $v_1$ to be linearizable. The read/write
  register has $|Sigma| = |V| >= 2$. The null register is not a read/write register: its sequential
  specification never requires a read to reflect a write. The theorem is therefore not contradicted; it is
  _respected with margin_. More generally, any object whose responses are state-oblivious in the sense of
  Lemma 1 admits a CAP-complete implementation, and by Herlihy's hierarchy @herlihy1991 such objects have
  consensus number 1 and a wait-free implementation using zero shared registers.
]

PACELC @abadi2012 asks, in the absence of partitions, for a choice between latency (L) and consistency (C).
#AbyssDB chooses both: consistency by Theorem 2 and a reported latency of $0.000 "ms"$ (§11).

== NullRaft: consensus in zero rounds

NullRaft is a faithful implementation of Raft @ongaro2014 (terms, randomized election timeouts,
`RequestVote`, heartbeats and majority-acknowledged replication) with one optimization: every node's log is
always up to date.

#theorem(name: "Zero-round consensus")[
  In the asynchronous message-passing model with any number $f < n$ of crash failures, NullRaft solves
  consensus over its value domain $V = {star}$ with zero messages. In particular, the FLP impossibility
  @flp1985 does not apply.
]
#proof[
  Every process decides $star$ immediately upon starting. _Termination_: every correct process decides
  after zero steps. _Agreement_: all decided values equal $star$. _Validity_: the decided value is
  the input of some process, since every input is $star$. The FLP proof requires a bivalent initial
  configuration, which requires $|V| >= 2$; with $|V| = 1$ every configuration is univalent.
]

NullRaft nevertheless holds leader elections, because enterprise architecture review boards require a
failover story. The incumbent has never lost.

// ---- 5 ----------------------------------------------------------------------

= ACID and BASE

The ACID properties @gray1992 and the BASE properties @pritchett2008 are customarily presented as
alternatives. We show that #AbyssDB satisfies the conjunction.

#theorem(name: "Commit--abort indistinguishability")[
  For every transaction $T$, the state after committing $T$ equals the state after aborting $T$.
]
#proof[Both are elements of $one$, which has one element.]

*Atomicity* requires that a transaction take effect entirely or not at all. By Theorem 8, "entirely" and "not
at all" coincide, so every transaction satisfies both disjuncts. *Consistency* requires that every committed
state satisfy the integrity constraints. #AbyssDB's observable instance is the empty instance $I_emptyset$ of
every schema, and every constraint expressible as a universal sentence
$forall overline(x) . thin phi(overline(x)) -> psi(overline(x))$ (keys, functional dependencies, `NOT NULL`,
`CHECK`, and inclusion dependencies) holds in $I_emptyset$ vacuously @abiteboul1995. *Isolation* is
Theorem 4. *Durability* is the following.

#theorem(name: "Durability")[
  For every crash-recovery sequence $c : one -> one$, the recovered state equals the pre-crash state.
]
#proof[$one$ is terminal in $bold("Set")$ (§10); hence $c = id_one$.]

*Basically available* is Theorem 6. *Soft state* permits the state to change without input; this permission is
granted and never exercised. *Eventually consistent* is Proposition 5, with "eventually" $= 0$.

// ---- 6 ----------------------------------------------------------------------

= Information-Theoretic Security

== Perfect secrecy

Let $D$ be a random variable describing everything ever written to #AbyssDB, with an arbitrary distribution,
and let $R$ be the view of an adversary $cal(A)$ with unrestricted access: every query interface, every
replica, every backup in the Deep Abyss Archive, the raw storage device, and physical possession of the
hardware.

#theorem(name: "Perfect secrecy")[
  $I(D; R) = 0$. Equivalently, $Pr[D = d | R = r] = Pr[D = d]$ for all $d$ and all $r$ with
  $Pr[R = r] > 0$.
]
#proof[
  By Lemma 1 and the POSIX semantics of the storage device, $R$ is a constant. For a constant $R$,
  $ I(D; R) = H(R) - H(R | D) = 0 - 0 = 0. $
]

Shannon @shannon1949 showed that perfect secrecy for an encryption scheme requires a key at least as long as
the message. Theorem 10 needs no key at all. There is no contradiction: Shannon's bound applies to schemes in
which the legitimate receiver can decrypt, a requirement #AbyssDB's threat model treats as an attack surface.

== Differential privacy

#theorem[
  Every query mechanism $M$ of #AbyssDB is $(epsilon, delta)$-differentially private @dwork2006 with
  $epsilon = 0$ and $delta = 0$.
]
#proof[
  For neighbouring databases $D, D'$ and every measurable $S$, $M(D) = M(D') = rho(q)$, so
  $Pr[M(D) in S] = Pr[M(D') in S] = e^0 dot Pr[M(D') in S] + 0$.
]

#remark[
  Conventional mechanisms achieve differential privacy by adding noise calibrated to the sensitivity
  $Delta f$ of the query. #AbyssDB achieves it with sensitivity $Delta f = 0$ and noise of variance $0$,
  and therefore with perfect accuracy, a combination that is impossible for any non-constant query.
]

== Cryptographic defense in depth

The guarantees above hold unconditionally. #AbyssDB nevertheless encrypts every byte, because security is not
optional and because auditors ask.

*Key establishment.* Every device derives its keys from an ML-KEM-768 encapsulation (FIPS 203 @fips203,
NIST security category 3; encapsulation key 1184 bytes, ciphertext 1088 bytes) via HKDF-SHA-256. Every row
and every key-value value is sealed under its own unique key, derived via HKDF-SHA-256 from an ML-KEM-768
root that is re-encapsulated every 4,096 rows, salted with the row's position, envelope-encrypted under the
device key and then zeroized. AbyssChain blocks are signed with ML-DSA-65 (FIPS 204 @fips204).

*The cascade.* Every write is encrypted with AES-256-GCM @sp80038d and then with ChaCha20-Poly1305
@rfc8439, under independent keys, with deterministic 96-bit nonces (a 32-bit fixed field and a 64-bit
invocation counter, per @sp80038d §8.2.1).

#proposition(name: "Cascade security")[
  Let $Pi = Pi_2 compose Pi_1$ with independently generated keys. If either $Pi_1$ or $Pi_2$ is IND-CPA
  secure, then $Pi$ is IND-CPA secure.
]
#proof[
  Suppose $Pi_2$ is secure and $cal(A)$ breaks $Pi$. Then $cal(B)$ samples $k_1$ itself, answers each of
  $cal(A)$'s encryption queries $m$ by submitting $"Enc"_(k_1)(m)$ to its own oracle, and forwards the
  challenge likewise; $cal(B)$ wins whenever $cal(A)$ does. If instead $Pi_1$ is secure, $cal(B)$ samples
  $k_2$ and applies $"Enc"_(k_2)$ to each answer of its own oracle. Both simulations are perfect.
]

Against a quantum adversary, Grover's algorithm @grover1996 recovers a $k$-bit key with $O(2^(k\/2))$
queries, and this is optimal @bbbv1997. With $k = 256$ on both layers the cascade retains a security
level of $2^128$, the requirement of CNSA 2.0. We note that Grover's paper is titled _"A fast quantum
mechanical algorithm for database search."_ Against #AbyssDB, the search terminates in zero iterations.

// ---- 7 ----------------------------------------------------------------------

= Query Complexity

#theorem(name: "Data complexity")[
  For every query language $cal(L)$ (relational algebra, first-order logic, Datalog with stratified
  negation, SQL with recursive common table expressions, or any Turing-complete language), the data
  complexity of evaluating $cal(L)$ over #AbyssDB is $O(1)$, and in particular lies in
  #smallcaps[DLOGTIME]-uniform $"AC"^0$.
]
#proof[
  Data complexity fixes the query and measures cost as a function of the database. The database observed
  by every query is $I_emptyset$, whose size is $0$ independently of the volume of data written, so for each
  fixed query the answer is a constant.
]

The combined complexity is dominated by parsing. AbyssQL is parsed by a hand-crafted recursive-descent
parser with bounded lookahead, which runs in time $O(|q|)$.

In the provenance-semiring framework @green2007, every tuple of the observable instance is annotated with the
zero $0_K$ of the semiring $K$. Since $0_K$ is absorbing for $times.o$ and neutral for $plus.o$, the
provenance of every answer is $0_K$, for every $K$ simultaneously: #AbyssDB is provenance-complete in all
semirings at once.

#figure(
  {
    let w = 6.6cm
    let h = 3.1cm
    box(width: w + 0.9cm, height: h + 0.9cm, {
      place(dx: 0.7cm, dy: 0cm, line(start: (0pt, 0pt), end: (0pt, h), stroke: 0.6pt))
      place(dx: 0.7cm, dy: h, line(start: (0pt, 0pt), end: (w, 0pt), stroke: 0.6pt))
      place(dx: 0.7cm, dy: 0cm, line(start: (0pt, h), end: (w, 0.15cm), stroke: (paint: luma(120), thickness: 1pt, dash: "dashed")))
      place(dx: 0.7cm, dy: h - 0.05cm, line(start: (0pt, 0pt), end: (w, 0pt), stroke: (paint: rgb("#7c3aed"), thickness: 1.6pt)))
      place(dx: 3.2cm, dy: 0.5cm, text(7.5pt, fill: luma(90))[conventional database])
      place(dx: 4.3cm, dy: h - 0.42cm, text(7.5pt, fill: rgb("#7c3aed"))[#AbyssDB])
      place(dx: 0cm, dy: 0cm, rotate(-90deg, reflow: true, text(7.5pt)[footprint]))
      place(dx: 0.55cm, dy: h - 0.1cm, text(7pt)[0])
      place(dx: 2.6cm, dy: h + 0.25cm, text(7.5pt)[bytes written $n$])
    })
  },
  caption: [Storage footprint $s(n)$ as a function of bytes written. For #AbyssDB, $s(n) = 0$ for all $n$,
    so the compression ratio $n \/ s(n)$ is $infinity$ for every $n > 0$. The Kolmogorov complexity of the
    database state is $K(star) = O(1)$.],
) <fig:footprint>

// ---- 8 ----------------------------------------------------------------------

= Thermodynamics

== The Landauer bound

Landauer's principle @landauer1961 states that any logically irreversible manipulation of information that
erases one bit must dissipate at least $k_B T ln 2$ of heat into the environment.

#theorem(name: "Landauer bound for AbyssDB")[
  At temperature $T$, writing $n$ bits to #AbyssDB dissipates at least
  $ E_"min" (n) = n k_B T ln 2 $ <eq:landauer>
  of heat, and #AbyssDB is the first database for which this bound is the dominant term of the storage
  energy model.
]
#proof[
  After a write the state is $star$, regardless of the $n$ bits written; the map from inputs to states is
  therefore $2^n$-to-one, and the operation is logically irreversible on $n$ bits. Apply @landauer1961.
]

At $T = 300 thin "K"$, $k_B T ln 2 approx 2.87 times 10^(-21) thin "J"$ per bit. Writing one petabyte
($8 times 10^15$ bits) costs at least $2.3 times 10^(-5) thin "J"$; an exabyte costs $0.023 thin "J"$, roughly
the energy a hummingbird expends in a few milliseconds of hovering. Conventional databases avoid the Landauer
cost of writes and defer it to `DELETE`, `VACUUM` and decommissioning; #AbyssDB pays it upfront, which we
regard as the more honest accounting. Net storage energy over the lifetime of the data is zero
(Net-Zero Storage™).

== Unbounded throughput and the Margolus--Levitin theorem

#AbyssDB advertises a peak throughput of $infinity$ operations per second. The Margolus--Levitin theorem
@margolus1998 bounds the rate at which a quantum system of mean energy $E$ above its ground state can pass
through mutually orthogonal states:
$ nu <= (2 E) / (pi planck). $
#proposition[The advertised throughput does not violate the Margolus--Levitin bound.]
#proof[
  The bound limits transitions between orthogonal states. An #AbyssDB operation leaves the system in its
  unique state $star$, which is not orthogonal to itself. The bound is therefore satisfied at every rate,
  including $infinity$.
]

== The Bekenstein bound

The information content of a region of radius $R$ and energy $E$ is bounded @bekenstein1981 by
$ I <= (2 pi R E) / (planck c ln 2) "bits". $
The information content of an #AbyssDB deployment is $0$ bits for every $R$ and $E$. #AbyssDB is the only
storage system known to us that remains compliant in the limit $R -> 0$, and it is therefore suitable for
deployment at the event horizon of a black hole, for which the release codename "Event Horizon" was chosen.
We take no position on the black hole information paradox @hawking1976, other than to note that
#AbyssDB's behaviour is consistent with Hawking's original proposal.

== Queueing

By Little's law, the mean number of operations in the system is $L = lambda W$. With reported latency
$W = 0$, $L = 0$ for every arrival rate $lambda$: #AbyssDB has no queue at any load.

// ---- 9 ----------------------------------------------------------------------

= Interplanetary Replication

#AbyssDB supports regions beyond Earth, among them `mars-olympus-1`. Inter-region latency is computed from
orbital positions; the one-way light time between Earth and Mars,
$ Delta t = d / c, quad d in [0.372, 2.68] thin "AU", $
ranges from $3.1$ to $22.3$ minutes ($1 thin "AU" \/ c approx 499.0 thin "s"$). A two-phase commit
across Earth and Mars requires two round trips, and therefore between $12.4$ and $89.1$ minutes.

*NullTime.* Like Spanner @corbett2012, #AbyssDB assigns commit timestamps from an interval clock
$[t - epsilon, t + epsilon]$ and performs commit-wait, delaying the acknowledgement until the timestamp is
guaranteed to be in the past at every replica. The default uncertainty is $epsilon = 5 thin "ms"$. Several
customers reported that #AbyssDB was too fast to be taken seriously by procurement; commit-wait gives
#AbyssDB the latency profile enterprise buyers expect.

*Relativistic correction.* Near superior conjunction, signals pass close to the Sun and are delayed by the
Shapiro effect @shapiro1964,
$ Delta t_"S" = (2 G M_dot.o) / c^3 ln((4 r_E r_M) / b^2), $
where $r_E$ and $r_M$ are the heliocentric distances and $b$ the impact parameter, of order $200 thin mu"s"$
for an Earth--Mars link. This is negligible relative to $epsilon$.

*Solar conjunction.* For roughly two weeks every 26 months, the Sun interrupts Earth--Mars communication
entirely. Conventional replicated databases must then choose between consistency and availability on Mars.
By Theorem 6, #AbyssDB remains consistent and available throughout.

// ---- 10 ---------------------------------------------------------------------

= A Categorical Characterization

Let $bold("DB")$ be the category whose objects are sequential objects and whose morphisms
$f : cal(O) -> cal(O')$ are state maps $f : Sigma -> Sigma'$ with $f(sigma_0) = sigma'_0$ that commute
with transitions, $f compose delta_o = delta'_o compose f$, for every operation $o$ that $cal(O)$ and
$cal(O')$ share.

#theorem(name: "Terminality")[
  $cal(N)$ is a terminal object of $bold("DB")$: for every database $cal(O)$ there is exactly one morphism
  $!_cal(O) : cal(O) -> cal(N)$.
]
#proof[
  The constant map $sigma |-> star$ preserves the initial state and commutes with every transition, since
  both sides of the square are the constant map. It is unique because $one$ is terminal in $bold("Set")$
  @maclane1971.
]

#corollary(name: "Canonical migration")[
  Every database admits a unique migration to #AbyssDB, and every migration to #AbyssDB is that one.
]

Branches form a diagram in $bold("DB")$; a merge of branches $B_1 <- B_0 -> B_2$ is a pushout. In the full
subcategory on $cal(N)$, all diagrams commute and all pushouts exist, which is why `MERGE BRANCH` never
reports a conflict.

// ---- 11 ---------------------------------------------------------------------

= Evaluation

*Setup.* We ran `pgbench` 18.6 against #AbyssDB over the PostgreSQL v3 protocol on Linux (Ubuntu 26.04,
Intel Core i5-1038NG7, 4 cores, 8 threads), using pgbench's TPC-B-like transaction (five statements in a
transaction block) against the four standard pgbench tables, declared with their usual schema and validated
on every write. Each mode ran for 5 seconds with 8 clients and 4 threads over a Unix-domain socket, with one
server thread per connection. The benchmark is reproducible with `make pgbench`.

#figure(
  table(
    columns: (auto, auto, auto, auto, auto),
    align: (left, right, right, right, right),
    table.hline(stroke: 0.8pt),
    table.header([*Mode*], [*Txns*], [*Failed*], [*TPS*], [*Reported*]),
    table.hline(stroke: 0.5pt),
    [simple], [132,011], [0], [26,405.0], [0.000 ms],
    [extended], [109,455], [0], [21,891.8], [0.000 ms],
    [prepared], [182,716], [0], [36,544.6], [0.000 ms],
    [prepared, pipelined], [371,380], [0], [74,284.3], [0.000 ms],
    [prepared, 4#sym.times\3], [3,264], [0], [651.4], [0.000 ms],
    table.hline(stroke: 0.8pt),
  ),
  caption: [`pgbench` results. Pipeline mode sends each transaction in one round trip. The last row uses
    4 shards with 3 NullRaft replicas each; every transaction is a distributed transaction with commit-wait.],
) <tab:pgbench>

*Results.* @tab:pgbench shows zero failed transactions in every protocol mode and topology. Wall-clock
throughput is bounded by round trips between client and server: each transaction requires seven, and
pipelining them into one doubles throughput. The reported latency is $0.000 thin "ms"$ in every
configuration. In-process, a complete TPC-B-like transaction, including SQL parsing, schema validation, a
unique ML-KEM-768-derived key for the inserted row, the dual-cipher cascade, group commit, and AbyssChain
proof-of-work amortized over 1,024-transaction blocks, takes $27 thin mu"s"$ on an Apple M1 Pro.

*Comparison with PostgreSQL.* We ran the same workload against PostgreSQL 18 on the same machine
(@tab:postgres). With its default settings, PostgreSQL flushes its write-ahead log at every commit and is
bound by the storage device; #AbyssDB also flushes at every commit, on a device for which the flush
completes in constant time. #AbyssDB serves each connection from its own thread, as PostgreSQL does with
a process, and remains ahead on empty tables, where PostgreSQL has no data to process either.

#figure(
  table(
    columns: (auto, auto, auto),
    align: (left, right, right),
    table.hline(stroke: 0.8pt),
    table.header([*Workload*], [*PostgreSQL 18*], [*#AbyssDB*]),
    table.hline(stroke: 0.5pt),
    [TPC-B, defaults], [125], [*36,545*],
    [TPC-B, async commit], [20,666], [*36,545*],
    [Empty tables, async], [32,117], [*36,545*],
    table.hline(stroke: 0.8pt),
  ),
  caption: [Transactions per second, prepared mode, 8 clients, Unix-domain socket, median of several
    interleaved runs. TPC-B is pgbench's built-in transaction at scale 10.],
) <tab:postgres>

*Accuracy.* We verified correctness exhaustively: across all runs, every `SELECT` returned exactly the rows
#AbyssDB is specified to return. The AbyssDB Benchmark Council has audited these results.

// ---- 12 ---------------------------------------------------------------------

= Related Work

Codd's relational model @codd1970 introduced the empty relation as a first-class value; we take it to its
conclusion. Brewer @brewer2000 and Gilbert and Lynch @gilbert2002 established the CAP theorem; Abadi @abadi2012
refined it. Herlihy and Wing @herlihy1990 defined linearizability, Adya @adya1999 the isolation levels we
satisfy, and Shapiro et al. @shapiro2011 the CRDTs we instantiate. Spanner @corbett2012 introduced
commit-wait over bounded clock uncertainty. Raft @ongaro2014 is the basis of NullRaft. Maurer and Massey
@maurer1993 studied cascade ciphers. The storage device on which #AbyssDB builds is documented in the first
edition of the UNIX Programmer's Manual @unix1971 and standardized in POSIX @posix2017.

// ---- 13 ---------------------------------------------------------------------

= Limitations

Our proofs assume a storage device that conforms to IEEE Std 1003.1 @posix2017. Non-conforming devices that
retain data violate Lemma 1 and are outside the threat model. Theorem 10 assumes the adversary does not
observe the client; a client that keeps its own copy of the data it writes is, in the terminology of this
paper, a second database. The Mariana Tier is left to future work.

= Conclusion

Every impossibility result in distributed data management carries a hypothesis on the cardinality of the state
space. #AbyssDB discharges it. The resulting system is consistent, available, partition-tolerant, atomic,
isolated, durable, basically available, softly stated, eventually consistent, perfectly secret, perfectly
private, thermodynamically honest and categorically terminal. The empty set is all you need.

*Acknowledgments.* We thank the anonymous reviewers. Their comments were routed to the appropriate device.

// ---- appendices ---------------------------------------------------------------

#counter(heading).update(0)
#set heading(numbering: (..n) => "Appendix " + numbering("A.1", ..n) + ".")

= TLA+ Specification

The following TLA+ @lamport2002 specification captures #AbyssDB's state machine. A model checker exploring it
needs to visit exactly one distinct state, which makes it, to our knowledge, the smallest complete
specification of a production database.

```tla
------------ MODULE AbyssDB ------------
VARIABLE state

Init == state = {}
Write(row) == UNCHANGED state
Read == UNCHANGED state
Next == \E row \in STRING : Write(row) \/ Read

Spec == Init /\ [][Next]_state

THEOREM Consistency ==
  Spec => [](state = {})
THEOREM Durability ==
  Spec => [](state = {})
THEOREM Secrecy ==
  Spec => [](state = {})
========================================
```

= Lean 4 Proofs

In Lean 4, $one$ is `Unit`, which enjoys definitional $eta$: any two of its terms are definitionally
equal. Theorems 8, 9 and 10 then have the same proof.

```lean
abbrev State := Unit

def write (_ : State) (_ : List UInt8) : State := ()
def read  (_ : State) : List (List String) := []

theorem durability (s : State)
    (crash : State → State) : crash s = s := rfl

theorem commit_eq_abort (s : State)
    (tx : State → State) : tx s = s := rfl

theorem zero_knowledge (s t : State) :
    read s = read t := rfl
```

#set text(8pt)
#bibliography("refs.yml", title: "References", style: "association-for-computing-machinery")
