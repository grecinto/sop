(function () {
  function goToOverview() {
    window.location.assign('/site/');
  }

  document.addEventListener('DOMContentLoaded', function () {
    const link = Array.from(document.querySelectorAll('.navbar__item.navbar__link')).find((element) => element.textContent?.trim() === 'Back to Overview');
    if (!link) {
      return;
    }

    link.setAttribute('href', '/site/');
    link.addEventListener('click', function (event) {
      event.preventDefault();
      goToOverview();
    });
  });
})();
