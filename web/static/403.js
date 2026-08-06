(function () {
  var p = new URLSearchParams(location.search);
  // Show the reason text in the generic banner if provided (login failures, etc.).
  var reasonEl = document.getElementById('generic-reason');
  var reason = p.get('reason');
  if (reason && reasonEl) {
    reasonEl.textContent = reason;
  }
  if (!p.has('banned')) { return; }
  document.getElementById('generic').style.display = 'none';
  document.getElementById('ban-info').style.display = '';
  document.getElementById('ban-status').textContent = p.get('permanent') === '1' ? '永久封禁' : '暂时封禁';
  var until = p.get('until');
  if (until && p.get('permanent') !== '1') {
    document.getElementById('ban-until-row').style.display = '';
    document.getElementById('ban-until').textContent = new Date(parseInt(until, 10) * 1000).toLocaleString();
  }
  var banReason = p.get('reason');
  if (banReason) {
    document.getElementById('ban-reason-row').style.display = '';
    document.getElementById('ban-reason').textContent = banReason;
  }
})();
