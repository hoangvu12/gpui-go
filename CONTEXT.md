# gpui-go language

Vocabulary for discussing a Go port of GPUI and its component ecosystem. Confirmed intent, current phase, and workflow choices live in [PROJECT.md](PROJECT.md).

## Language

- **Core parity (1:1):** the goal that the Go port reproduce the reference Rust GPUI framework's capabilities and observable behavior, subject to explicit compatibility exceptions. It describes the framework itself; Base and other component libraries are separate consumers.
- **Visual closeness:** how closely rendered output resembles the Rust reference, distinct from functional coverage. It permits bounded rendering differences; the accepted target and evaluation policy belong in the parity and conformance decisions.
- **Syntax fidelity:** familiar fluent element and styling calls, composable children, typed view/context interaction, and recognizable names adapted to Go spelling.
- **Behavioral fidelity:** observable state, identity, invalidation, event propagation, focus, layout, rendering, and resource-lifetime behavior covered by an explicit compatibility contract.
- **Ecosystem portability:** the ability to translate libraries such as GPUI Base into Go against that contract. Rust crates do not become directly usable from Go.
- **Backend:** the platform and rendering implementation underneath the port's public interface. Existing Go libraries are candidates for reuse, not the definition of the port.
- **Conformance corpus:** pinned upstream examples and behavioral scenarios used to specify what the port must reproduce.

**Entity**:
Persistent application state with a logical identity, independent of any one rendered placement. A model entity need not be renderable.

**Component recipe**:
A description consumed to construct one placement in an element tree. Reusing the underlying application data is distinct from attaching the same mutable recipe again.
_Avoid_: Persistent component state, entity.

**View identity**:
The identity that scopes a rendered view's retained element state within its parent path. Sharing model state does not require sharing a view identity.

**Entity event**:
A particular occurrence emitted by an entity with a declared payload type and delivered to subscribers. It is distinct from a notification that the entity's state should be reconsidered.

**Action**:
A typed command routed through a window's element dispatch path; keyboard bindings may select it using focus and key context. It is distinct from an entity event reporting something that already happened.

**Event declaration**:
The association between an emitting state type and an entity-event payload type. Separate declarations of the same association describe the same event stream for a given entity.

**Ownership lease**:
One independently releasable claim keeping an entity or resource logically alive. Aliases of one lease share its release state; retaining creates another lease.

**Owner scope**:
A named lifetime that groups leases, registrations and tasks for deterministic cancellation and release. Closing one scope does not release independent leases held by another.

**Logical release**:
The irreversible end of an entity's eligibility for new access or weak upgrade once its final strong lease ends. Physical memory reclamation and native-resource retirement can occur later.

**Effect cycle**:
The foreground processing of queued notifications, events and deferred work after an outer update, including work appended during delivery. It is distinct from a rendered frame.

**Retained element state**:
State associated with an element's identity path and type within one window, carried across committed frames while that identity remains in use.

**Layout node**:
A node participating in one layout computation and its frame-local geometry. Its identity is distinct from persistent entity identity and retained element-state identity.

**Logical pixel**:
A layout coordinate unit before display scaling. A device pixel is a physical display-grid unit; the scale factor relates the two.

**Intrinsic measurement**:
An element's size response to known dimensions and available-space constraints, including minimum-content and maximum-content modes. It is distinct from the final positioned bounds.

**Inline fragment**:
One positioned portion of content in a text flow. An element can occupy multiple fragments rather than one ordinary rectangular layout placement.

**Text layout**:
Shaped text and its geometry, including clusters, lines, caret movement, selection and hit testing. Text offsets in layout and UTF-16 document offsets in native input are distinct units.

**Glyph raster**:
CPU pixel coverage or color for a selected font glyph at a given size, scale and raster style. It is distinct from text shaping and from its uploaded GPU atlas allocation.

**Published accessibility tree**:
The semantic node snapshot available to native accessibility providers. Queries can read it independently of mutable application state; requested actions return through foreground dispatch.

**Paint command stream**:
Ordered drawing operations and resource references produced during element painting, including layer changes and retained-scene replay. It is distinct from the GPU command stream.

**Scene plan**:
The ordered batches and filter-target transitions derived from a finished scene. It describes rendering work without owning application or element state.

**Submission retirement**:
The end of an accepted native work item's ownership obligations after confirmed completion or a safe abort. Acceptance, presentation and device-generation invalidation are separate events.

**Native artifact**:
A particular prebuilt binary identified by its complete content hash, source/build provenance, target, private ABI and compiled capabilities. It is distinct from the logical services packaged inside it.

**Compiled capability**:
A service or operation included in a native artifact. Its presence is distinct from runtime availability on a particular OS, device or user session.
