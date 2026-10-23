var gpui_script = (() => {
  var __defProp = Object.defineProperty;
  var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
  var __getOwnPropNames = Object.getOwnPropertyNames;
  var __hasOwnProp = Object.prototype.hasOwnProperty;
  var __export = (target, all) => {
    for (var name in all)
      __defProp(target, name, { get: all[name], enumerable: true });
  };
  var __copyProps = (to, from, except, desc) => {
    if (from && typeof from === "object" || typeof from === "function") {
      for (let key of __getOwnPropNames(from))
        if (!__hasOwnProp.call(to, key) && key !== except)
          __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
    }
    return to;
  };
  var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

  // app.tsx
  var app_exports = {};
  __export(app_exports, {
    App: () => App
  });
  function App() {
    const clicks = state("clicks", 0);
    return /* @__PURE__ */ h(
      "div",
      {
        id: "page",
        className: "flex items-center justify-center",
        style: { width: 480, height: 320, background: "#1e293b" }
      },
      /* @__PURE__ */ h(
        "div",
        {
          id: "card",
          className: "flex items-center justify-center w-[160px] h-[96px] rounded-lg",
          style: { background: "#2563eb", color: "#ffffff", fontSize: 20 },
          onClick: () => clicks.set(clicks.get() + 1)
        },
        /* @__PURE__ */ h("text", { id: "label" }, bind(clicks, (n) => `Hello TSX! ${n}`))
      )
    );
  }
  return __toCommonJS(app_exports);
})();
//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsiYXBwLnRzeCJdLAogICJzb3VyY2VzQ29udGVudCI6IFsiLy8gVGhlIHRzeC1oZWxsbyB2aWV3IChBRFIgMDAwMydzIGF1dGhvcmluZyBzdXJmYWNlKTogSlNYIGVsZW1lbnRzLCBhXG4vLyBUYWlsd2luZCBjbGFzc05hbWUsIGEgQ1NTLW5hbWVkIHN0eWxlIG9iamVjdCBhbmQga2V5ZWQgc3RhdGUgd2l0aCBhXG4vLyBib3VuZCBsYWJlbCBcdTIwMTQgdGhlIHNhbWUgc2hhcGUgYSBicm93c2VyIGNvbXBvbmVudCB3b3VsZCBoYXZlLlxuLy9cbi8vIFRoZSBwYWdlIGNlbnRlcnMgdGhlIGNhcmQ7IGNsaWNraW5nIHRoZSBjYXJkIGNvdW50cy4gVGhlIGxhYmVsIGlzIGFcbi8vIGJvdW5kIHZhbHVlIG9uIHRoZSBrZXllZCBzdGF0ZSwgc28gYSBjbGljayByZWNvbXB1dGVzIG9ubHkgdGhhdFxuLy8gbGFiZWwgKHRoZSBob3N0J3MgZmFzdCBwYXRoKSwgbmV2ZXIgdGhlIHdob2xlIHRyZWUuXG5leHBvcnQgZnVuY3Rpb24gQXBwKCkge1xuXHRjb25zdCBjbGlja3MgPSBzdGF0ZShcImNsaWNrc1wiLCAwKTtcblx0cmV0dXJuIChcblx0XHQ8ZGl2XG5cdFx0XHRpZD1cInBhZ2VcIlxuXHRcdFx0Y2xhc3NOYW1lPVwiZmxleCBpdGVtcy1jZW50ZXIganVzdGlmeS1jZW50ZXJcIlxuXHRcdFx0c3R5bGU9e3sgd2lkdGg6IDQ4MCwgaGVpZ2h0OiAzMjAsIGJhY2tncm91bmQ6IFwiIzFlMjkzYlwiIH19XG5cdFx0PlxuXHRcdFx0PGRpdlxuXHRcdFx0XHRpZD1cImNhcmRcIlxuXHRcdFx0XHRjbGFzc05hbWU9XCJmbGV4IGl0ZW1zLWNlbnRlciBqdXN0aWZ5LWNlbnRlciB3LVsxNjBweF0gaC1bOTZweF0gcm91bmRlZC1sZ1wiXG5cdFx0XHRcdHN0eWxlPXt7IGJhY2tncm91bmQ6IFwiIzI1NjNlYlwiLCBjb2xvcjogXCIjZmZmZmZmXCIsIGZvbnRTaXplOiAyMCB9fVxuXHRcdFx0XHRvbkNsaWNrPXsoKSA9PiBjbGlja3Muc2V0KGNsaWNrcy5nZXQoKSArIDEpfVxuXHRcdFx0PlxuXHRcdFx0XHQ8dGV4dCBpZD1cImxhYmVsXCI+e2JpbmQoY2xpY2tzLCAobjogbnVtYmVyKSA9PiBgSGVsbG8gVFNYISAke259YCl9PC90ZXh0PlxuXHRcdFx0PC9kaXY+XG5cdFx0PC9kaXY+XG5cdCk7XG59XG4iXSwKICAibWFwcGluZ3MiOiAiOzs7Ozs7Ozs7Ozs7Ozs7Ozs7OztBQUFBO0FBQUE7QUFBQTtBQUFBO0FBT08sV0FBUyxNQUFNO0FBQ3JCLFVBQU0sU0FBUyxNQUFNLFVBQVUsQ0FBQztBQUNoQyxXQUNDO0FBQUEsTUFBQztBQUFBO0FBQUEsUUFDQSxJQUFHO0FBQUEsUUFDSCxXQUFVO0FBQUEsUUFDVixPQUFPLEVBQUUsT0FBTyxLQUFLLFFBQVEsS0FBSyxZQUFZLFVBQVU7QUFBQTtBQUFBLE1BRXhEO0FBQUEsUUFBQztBQUFBO0FBQUEsVUFDQSxJQUFHO0FBQUEsVUFDSCxXQUFVO0FBQUEsVUFDVixPQUFPLEVBQUUsWUFBWSxXQUFXLE9BQU8sV0FBVyxVQUFVLEdBQUc7QUFBQSxVQUMvRCxTQUFTLE1BQU0sT0FBTyxJQUFJLE9BQU8sSUFBSSxJQUFJLENBQUM7QUFBQTtBQUFBLFFBRTFDLGtCQUFDLFVBQUssSUFBRyxXQUFTLEtBQUssUUFBUSxDQUFDLE1BQWMsY0FBYyxDQUFDLEVBQUUsQ0FBRTtBQUFBLE1BQ2xFO0FBQUEsSUFDRDtBQUFBLEVBRUY7IiwKICAibmFtZXMiOiBbXQp9Cg==
