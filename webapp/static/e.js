// Plain Old Analytics tracking snippet.
//
// Usage:
//   <script src="/analytics/e.js"></script>
//   <script>
//     plainoldanalytics.init_session_recording();                          // starts rrweb session recording
//     plainoldanalytics.track('signup', {plan: 'pro'});  // custom events
//   </script>
//
// Visitor identity is established by the SERVER: the analytics middleware
// sets the long-lived poa_visitor cookie and session-only poa_session cookie
// on the first response. This snippet only reads them — it never mints IDs.
(function () {
	'use strict';
	var poa = window.plainoldanalytics = window.poa = window.plainoldanalytics || window.poa || {};
	if (poa._loaded) return;
	poa._loaded = true;

	// The API base is wherever this script was served from.
	var script = document.currentScript;
	var base = script ? script.src.slice(0, script.src.lastIndexOf('/')) : '/analytics';

	// rrweb is bundled with the analytics app itself, so recording works
	// without any CDN.
	var RRWEB_SRC = base + '/static/vendor/rrweb.min.js';
	var COOKIE = 'poa_visitor';
	var SESSION_COOKIE = 'poa_session';

	// Visitor IDs are integers minted by the server; the cookie carries the
	// decimal form, and so does the JSON we send (JS numbers lose precision
	// past 2^53).
	function visitor() {
		var m = document.cookie.match('(?:^|; )' + COOKIE + '=([0-9]+)');
		return m ? m[1] : '';
	}

	function session() {
		var m = document.cookie.match('(?:^|; )' + SESSION_COOKIE + '=([0-9]+)');
		return m ? m[1] : '';
	}

	var pending = [];

	function send(path, body, beacon) {
		var json = JSON.stringify(body);
		if (beacon && navigator.sendBeacon) {
			navigator.sendBeacon(base + path, new Blob([json], { type: 'application/json' }));
			return;
		}
		fetch(base + path, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: json,
			keepalive: true,
		}).catch(function () {});
	}

	poa.track = function (name, props) {
		send('/e', {
			name: name,
			props: props || {},
			visitor: visitor(),
			session: session(),
			path: location.pathname,
		});
	};

	function flush(beacon) {
		if (!pending.length || !visitor()) return;
		var batch = pending;
		pending = [];
		send('/replay', { visitor: visitor(), session: session(), events: batch }, beacon);
	}

	poa.init_session_recording = poa.init = function (opts) {
		opts = opts || {};
		if (opts.replay === false) return;
		var s = document.createElement('script');
		s.src = opts.rrweb || RRWEB_SRC;
		s.onload = function () {
			window.rrweb.record({
				emit: function (event) { pending.push(event); },
			});
			setInterval(function () { flush(false); }, opts.flushInterval || 5000);
			window.addEventListener('visibilitychange', function () {
				if (document.visibilityState === 'hidden') flush(true);
			});
		};
		document.head.appendChild(s);
	};
})();
