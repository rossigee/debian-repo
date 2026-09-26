(function() {
  const btn = document.getElementById('user-dropdown-btn');
  const dropdown = document.getElementById('user-dropdown');

  if (!btn || !dropdown) return;

  // Toggle dropdown on button click
  btn.addEventListener('click', (e) => {
    e.stopPropagation();
    dropdown.classList.toggle('active');
    btn.classList.toggle('active');
  });

  // Close dropdown when clicking outside
  document.addEventListener('click', () => {
    dropdown.classList.remove('active');
    btn.classList.remove('active');
  });

  // Close dropdown when clicking a link inside it
  dropdown.querySelectorAll('a').forEach(link => {
    link.addEventListener('click', () => {
      dropdown.classList.remove('active');
      btn.classList.remove('active');
    });
  });
})();
