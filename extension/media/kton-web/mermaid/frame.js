// The sandboxed Mermaid frame (classic script). It turns Mermaid source into SVG text and answers over the
// MessagePort the composition frame hands it; it never posts to its parent window.
//
// - It takes the FIRST port it is given and ignores every later window message (C1.5 v3: the one
//   postMessage(port, "*") the contract allows carries only that port).
// - htmlLabels false (also flowchart.htmlLabels): labels are SVG <text>, no foreignObject, so the SVG
//   draws the same as an <img>, in rsvg, Inkscape and LibreOffice (H12, measured by userinterface).
// - The three fixes Mermaid 11.4.1 needs with htmlLabels false (kton-web review H12):
//   1. one text node per line: Mermaid puts every word in its own <tspan> with a leading space, which
//      rsvg collapses ("BugReporting"); the line tspans stay, their word tspans become one text node;
//   2. entities decoded once: "&" arrives escaped twice and would show as "&amp;";
//   3. width/height from the viewBox: Mermaid writes width="100%", which an <img> reads as 300x150.
// Answer: {id, svg, width, height, labels} or {id, error}.
(function () {
  "use strict";
  var n = 0;
  var port = null;
  mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: "neutral", htmlLabels: false, flowchart: { htmlLabels: false } });
  var ENT = { amp: "&", lt: "<", gt: ">", quot: '"', "#39": "'" };
  function fix(svgText) {
    var holder = document.createElement("div");
    holder.innerHTML = svgText; // the frame is sandboxed and holds nothing else; the result leaves as text
    document.body.appendChild(holder);
    var svg = holder.querySelector("svg");
    var lines = svg.querySelectorAll("tspan.text-outer-tspan");
    for (var i = 0; i < lines.length; i++) {
      var t = lines[i].textContent.replace(/&(amp|lt|gt|quot|#39);/g, function (m, e) { return ENT[e]; });
      while (lines[i].firstChild) lines[i].removeChild(lines[i].firstChild);
      lines[i].appendChild(document.createTextNode(t));
    }
    var vb = (svg.getAttribute("viewBox") || "").split(/\s+/).map(Number);
    var w = null, h = null;
    if (vb.length === 4 && vb[2] > 0 && vb[3] > 0) {
      w = Math.ceil(vb[2]);
      h = Math.ceil(vb[3]);
      svg.setAttribute("width", String(w));
      svg.setAttribute("height", String(h));
      svg.style.removeProperty("max-width");
    }
    var labels = [];
    var ls = svg.querySelectorAll("g.label, g.edgeLabel, g.cluster-label");
    for (var j = 0; j < ls.length; j++) {
      // a wrapped label is one outer tspan per line: the lines join with a space
      var outer = ls[j].querySelectorAll("tspan.text-outer-tspan");
      var raw = outer.length ? Array.prototype.map.call(outer, function (o) { return o.textContent; }).join(" ") : ls[j].textContent;
      var s = raw.replace(/\s+/g, " ").trim();
      if (s) labels.push(s);
    }
    var out = { svg: new XMLSerializer().serializeToString(svg), width: w, height: h, labels: labels, foreignObjects: svg.querySelectorAll("foreignObject").length };
    holder.remove();
    return out;
  }
  window.addEventListener("message", function (ev) {
    if (port) return; // the first port only
    if (!ev.data || ev.data.type !== "kton:mermaid-port" || !ev.ports || !ev.ports[0]) return;
    port = ev.ports[0];
    port.onmessage = function (e) {
      var id = e.data && e.data.id;
      var source = e.data && e.data.source;
      if (typeof source !== "string") return port.postMessage({ id: id, error: "source is a string" });
      mermaid.render("kton-m" + ++n, source).then(
        function (r) {
          var a = fix(r.svg);
          a.id = id;
          port.postMessage(a);
        },
        function (err) {
          port.postMessage({ id: id, error: String((err && err.message) || err) });
        },
      );
    };
    port.postMessage({ type: "ready" });
  });
})();
