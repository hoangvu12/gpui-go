// Type definitions for the probe's script surface. A real layer would
// generate this from the Go lowering tables (as zgui generates
// zgui.d.luau) and fail a test when the host accepts something the types
// lack.

type Px = number | `${number}px`;
type Color = `#${string}`;

/** The CSS subset the probe lowers (camelCase, as in React). */
interface CSSProperties {
  display?: "flex" | "block";
  flexDirection?: "row" | "column";
  alignItems?: "center" | "flex-start" | "start";
  justifyContent?: "center" | "space-between";
  flex?: 1;
  gap?: Px;
  width?: Px | "100%";
  height?: Px | "100%";
  minHeight?: Px;
  /** One, two or four lengths: "6px 8px". */
  padding?: Px | `${number}px ${number}px` | `${number}px ${number}px ${number}px ${number}px`;
  background?: Color;
  color?: Color;
  borderRadius?: Px;
  fontSize?: Px;
  fontWeight?: 100 | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900;
  opacity?: number;
  overflowY?: "scroll" | "auto";
}

interface Binding<T> {
  readonly __binding: T;
}

type Child = GpuiElement | string | number | boolean | null | undefined | Binding<string> | Child[];

interface BaseProps {
  key?: string;
  /** Tailwind classes, applied by gpui's Styled.Class. */
  className?: string;
  style?: CSSProperties;
  onClick?: () => void;
  children?: Child;
}

declare const __gpuiElement: unique symbol;
interface GpuiElement {
  readonly [__gpuiElement]: true;
}

declare namespace JSX {
  type Element = GpuiElement;
  interface IntrinsicElements {
    div: BaseProps;
    text: BaseProps;
  }
  interface ElementChildrenAttribute {
    children: {};
  }
  interface IntrinsicAttributes {
    key?: string;
  }
}

declare function h(type: unknown, props: unknown, ...children: unknown[]): GpuiElement;
declare const Fragment: unique symbol;

/** Host-owned keyed state, React-style: setting it re-describes the tree. A reload keeps the value. */
declare function useState<T>(key: string, init: T): [T, (value: T) => void];

/** Host-owned keyed state, zgui-style: setting it recomputes only its bindings. */
interface State<T> {
  readonly key: string;
  get(): T;
  set(value: T): void;
}
declare function state<T>(key: string, init: T): State<T>;

/** A computed value: the host calls fn again only when the state changes. */
declare function bind<T>(s: State<T>, fn: (value: T) => string): Binding<string>;
