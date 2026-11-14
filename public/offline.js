// The offline page the service worker shows for a navigation without a
// network. Reloading retries the address the visitor asked for; it reloads by
// itself when the browser reports the connection is back.
(function () {
  var retry = document.getElementById('offline-retry')
  var status = document.getElementById('offline-status')
  function again() {
    if (status) status.textContent = 'Reconnecting…'
    window.location.reload()
  }
  if (retry) retry.addEventListener('click', again)
  window.addEventListener('online', again)
})()
