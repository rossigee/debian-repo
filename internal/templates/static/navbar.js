(function() {
  const burger = document.getElementById('burger');
  const nav = document.querySelector('.nav-links');

  burger?.addEventListener('click', () => {
    nav?.classList.toggle('active');
    burger.classList.toggle('active');
  });
})();
