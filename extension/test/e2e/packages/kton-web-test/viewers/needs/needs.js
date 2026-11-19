// needs: a viewer that requires product@1. In a host that does not provide it, the composition hides it
// behind a counted placeholder ("1 block hidden: needs product@1"); a data capability hides a block.
export function mount(el, ctx) {
  el.textContent = "needs: mounted";
  ctx.request("product@1", { path: "x", hash: "sha256:" + "0".repeat(64) }).then(
    (r) => (el.textContent = `needs: ${JSON.stringify(r)}`),
    (e) => (el.textContent = `needs: ${e.message}`),
  );
}
