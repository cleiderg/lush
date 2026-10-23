/** Visible OpenStreetMap credit (ODbL requirement). Returns a function that removes it. */
export function showAttribution(parent: HTMLElement = document.body): () => void {
  const el = document.createElement('div');
  el.id = 'osm-attribution';
  el.style.cssText =
    'position:fixed;left:50%;bottom:2px;transform:translateX(-50%);font:10px sans-serif;' +
    'color:#fff;background:rgba(0,0,0,.45);padding:1px 6px;border-radius:3px;z-index:5;white-space:nowrap;';
  const link = document.createElement('a');
  link.href = 'https://www.openstreetmap.org/copyright';
  link.target = '_blank';
  link.rel = 'noopener noreferrer';
  link.style.color = 'inherit';
  link.textContent = '© OpenStreetMap contributors';
  el.appendChild(link);
  parent.appendChild(el);
  return () => el.remove();
}
