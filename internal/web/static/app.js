// Responsive top navigation. On small screens the menu collapses behind a
// hamburger and opens as a full-screen sheet (with the action buttons stacked
// at the bottom). Closes on link tap or Escape.
(function () {
  'use strict';

  var burger = document.getElementById('nav-burger');
  var sheet = document.getElementById('nav-sheet');
  if (!burger || !sheet) return;

  function close() {
    sheet.classList.remove('open');
    burger.setAttribute('aria-expanded', 'false');
  }

  burger.addEventListener('click', function () {
    var open = sheet.classList.toggle('open');
    burger.setAttribute('aria-expanded', open ? 'true' : 'false');
  });

  sheet.addEventListener('click', function (e) {
    if (e.target.closest('a, button')) close();
  });

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && sheet.classList.contains('open')) close();
  });
})();