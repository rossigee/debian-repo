(function() {
  const toggle = document.getElementById('theme-toggle');
  const root = document.documentElement;
  const stored = localStorage.getItem('theme') || 'auto';
  const prefersLight = window.matchMedia('(prefers-color-scheme: light)').matches;

  if (stored === 'dark' || (stored === 'auto' && !prefersLight)) {
    root.setAttribute('data-theme', 'dark');
  }

  toggle?.addEventListener('click', () => {
    const current = root.getAttribute('data-theme') || 'light';
    const next = current === 'dark' ? 'light' : 'dark';
    root.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
  });
})();
