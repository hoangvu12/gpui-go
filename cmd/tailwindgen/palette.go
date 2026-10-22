// Code generated from Tailwind CSS v3.4.17 src/public/colors.js (MIT,
// Copyright (c) Tailwind Labs, Inc.; see THIRD_PARTY_NOTICES.md) by a
// one-time conversion. It is tailwindgen's committed source of truth for
// the default color palette; edit only to follow a deliberate palette update.

package main

// families lists the palette families in Tailwind's order.
var families = []string{"slate", "gray", "zinc", "neutral", "stone", "red", "orange", "amber", "yellow", "lime", "green", "emerald", "teal", "cyan", "sky", "blue", "indigo", "violet", "purple", "fuchsia", "pink", "rose"}

// shades lists the shade steps every family defines.
var shades = []string{"50", "100", "200", "300", "400", "500", "600", "700", "800", "900", "950"}

// palette maps "family-shade" to its sRGB value.
var palette = map[string]uint32{
	"slate-50": 0xf8fafc, "slate-100": 0xf1f5f9, "slate-200": 0xe2e8f0, "slate-300": 0xcbd5e1, "slate-400": 0x94a3b8, "slate-500": 0x64748b, "slate-600": 0x475569, "slate-700": 0x334155, "slate-800": 0x1e293b, "slate-900": 0x0f172a, "slate-950": 0x020617,
	"gray-50": 0xf9fafb, "gray-100": 0xf3f4f6, "gray-200": 0xe5e7eb, "gray-300": 0xd1d5db, "gray-400": 0x9ca3af, "gray-500": 0x6b7280, "gray-600": 0x4b5563, "gray-700": 0x374151, "gray-800": 0x1f2937, "gray-900": 0x111827, "gray-950": 0x030712,
	"zinc-50": 0xfafafa, "zinc-100": 0xf4f4f5, "zinc-200": 0xe4e4e7, "zinc-300": 0xd4d4d8, "zinc-400": 0xa1a1aa, "zinc-500": 0x71717a, "zinc-600": 0x52525b, "zinc-700": 0x3f3f46, "zinc-800": 0x27272a, "zinc-900": 0x18181b, "zinc-950": 0x09090b,
	"neutral-50": 0xfafafa, "neutral-100": 0xf5f5f5, "neutral-200": 0xe5e5e5, "neutral-300": 0xd4d4d4, "neutral-400": 0xa3a3a3, "neutral-500": 0x737373, "neutral-600": 0x525252, "neutral-700": 0x404040, "neutral-800": 0x262626, "neutral-900": 0x171717, "neutral-950": 0x0a0a0a,
	"stone-50": 0xfafaf9, "stone-100": 0xf5f5f4, "stone-200": 0xe7e5e4, "stone-300": 0xd6d3d1, "stone-400": 0xa8a29e, "stone-500": 0x78716c, "stone-600": 0x57534e, "stone-700": 0x44403c, "stone-800": 0x292524, "stone-900": 0x1c1917, "stone-950": 0x0c0a09,
	"red-50": 0xfef2f2, "red-100": 0xfee2e2, "red-200": 0xfecaca, "red-300": 0xfca5a5, "red-400": 0xf87171, "red-500": 0xef4444, "red-600": 0xdc2626, "red-700": 0xb91c1c, "red-800": 0x991b1b, "red-900": 0x7f1d1d, "red-950": 0x450a0a,
	"orange-50": 0xfff7ed, "orange-100": 0xffedd5, "orange-200": 0xfed7aa, "orange-300": 0xfdba74, "orange-400": 0xfb923c, "orange-500": 0xf97316, "orange-600": 0xea580c, "orange-700": 0xc2410c, "orange-800": 0x9a3412, "orange-900": 0x7c2d12, "orange-950": 0x431407,
	"amber-50": 0xfffbeb, "amber-100": 0xfef3c7, "amber-200": 0xfde68a, "amber-300": 0xfcd34d, "amber-400": 0xfbbf24, "amber-500": 0xf59e0b, "amber-600": 0xd97706, "amber-700": 0xb45309, "amber-800": 0x92400e, "amber-900": 0x78350f, "amber-950": 0x451a03,
	"yellow-50": 0xfefce8, "yellow-100": 0xfef9c3, "yellow-200": 0xfef08a, "yellow-300": 0xfde047, "yellow-400": 0xfacc15, "yellow-500": 0xeab308, "yellow-600": 0xca8a04, "yellow-700": 0xa16207, "yellow-800": 0x854d0e, "yellow-900": 0x713f12, "yellow-950": 0x422006,
	"lime-50": 0xf7fee7, "lime-100": 0xecfccb, "lime-200": 0xd9f99d, "lime-300": 0xbef264, "lime-400": 0xa3e635, "lime-500": 0x84cc16, "lime-600": 0x65a30d, "lime-700": 0x4d7c0f, "lime-800": 0x3f6212, "lime-900": 0x365314, "lime-950": 0x1a2e05,
	"green-50": 0xf0fdf4, "green-100": 0xdcfce7, "green-200": 0xbbf7d0, "green-300": 0x86efac, "green-400": 0x4ade80, "green-500": 0x22c55e, "green-600": 0x16a34a, "green-700": 0x15803d, "green-800": 0x166534, "green-900": 0x14532d, "green-950": 0x052e16,
	"emerald-50": 0xecfdf5, "emerald-100": 0xd1fae5, "emerald-200": 0xa7f3d0, "emerald-300": 0x6ee7b7, "emerald-400": 0x34d399, "emerald-500": 0x10b981, "emerald-600": 0x059669, "emerald-700": 0x047857, "emerald-800": 0x065f46, "emerald-900": 0x064e3b, "emerald-950": 0x022c22,
	"teal-50": 0xf0fdfa, "teal-100": 0xccfbf1, "teal-200": 0x99f6e4, "teal-300": 0x5eead4, "teal-400": 0x2dd4bf, "teal-500": 0x14b8a6, "teal-600": 0x0d9488, "teal-700": 0x0f766e, "teal-800": 0x115e59, "teal-900": 0x134e4a, "teal-950": 0x042f2e,
	"cyan-50": 0xecfeff, "cyan-100": 0xcffafe, "cyan-200": 0xa5f3fc, "cyan-300": 0x67e8f9, "cyan-400": 0x22d3ee, "cyan-500": 0x06b6d4, "cyan-600": 0x0891b2, "cyan-700": 0x0e7490, "cyan-800": 0x155e75, "cyan-900": 0x164e63, "cyan-950": 0x083344,
	"sky-50": 0xf0f9ff, "sky-100": 0xe0f2fe, "sky-200": 0xbae6fd, "sky-300": 0x7dd3fc, "sky-400": 0x38bdf8, "sky-500": 0x0ea5e9, "sky-600": 0x0284c7, "sky-700": 0x0369a1, "sky-800": 0x075985, "sky-900": 0x0c4a6e, "sky-950": 0x082f49,
	"blue-50": 0xeff6ff, "blue-100": 0xdbeafe, "blue-200": 0xbfdbfe, "blue-300": 0x93c5fd, "blue-400": 0x60a5fa, "blue-500": 0x3b82f6, "blue-600": 0x2563eb, "blue-700": 0x1d4ed8, "blue-800": 0x1e40af, "blue-900": 0x1e3a8a, "blue-950": 0x172554,
	"indigo-50": 0xeef2ff, "indigo-100": 0xe0e7ff, "indigo-200": 0xc7d2fe, "indigo-300": 0xa5b4fc, "indigo-400": 0x818cf8, "indigo-500": 0x6366f1, "indigo-600": 0x4f46e5, "indigo-700": 0x4338ca, "indigo-800": 0x3730a3, "indigo-900": 0x312e81, "indigo-950": 0x1e1b4b,
	"violet-50": 0xf5f3ff, "violet-100": 0xede9fe, "violet-200": 0xddd6fe, "violet-300": 0xc4b5fd, "violet-400": 0xa78bfa, "violet-500": 0x8b5cf6, "violet-600": 0x7c3aed, "violet-700": 0x6d28d9, "violet-800": 0x5b21b6, "violet-900": 0x4c1d95, "violet-950": 0x2e1065,
	"purple-50": 0xfaf5ff, "purple-100": 0xf3e8ff, "purple-200": 0xe9d5ff, "purple-300": 0xd8b4fe, "purple-400": 0xc084fc, "purple-500": 0xa855f7, "purple-600": 0x9333ea, "purple-700": 0x7e22ce, "purple-800": 0x6b21a8, "purple-900": 0x581c87, "purple-950": 0x3b0764,
	"fuchsia-50": 0xfdf4ff, "fuchsia-100": 0xfae8ff, "fuchsia-200": 0xf5d0fe, "fuchsia-300": 0xf0abfc, "fuchsia-400": 0xe879f9, "fuchsia-500": 0xd946ef, "fuchsia-600": 0xc026d3, "fuchsia-700": 0xa21caf, "fuchsia-800": 0x86198f, "fuchsia-900": 0x701a75, "fuchsia-950": 0x4a044e,
	"pink-50": 0xfdf2f8, "pink-100": 0xfce7f3, "pink-200": 0xfbcfe8, "pink-300": 0xf9a8d4, "pink-400": 0xf472b6, "pink-500": 0xec4899, "pink-600": 0xdb2777, "pink-700": 0xbe185d, "pink-800": 0x9d174d, "pink-900": 0x831843, "pink-950": 0x500724,
	"rose-50": 0xfff1f2, "rose-100": 0xffe4e6, "rose-200": 0xfecdd3, "rose-300": 0xfda4af, "rose-400": 0xfb7185, "rose-500": 0xf43f5e, "rose-600": 0xe11d48, "rose-700": 0xbe123c, "rose-800": 0x9f1239, "rose-900": 0x881337, "rose-950": 0x4c0519,
}
