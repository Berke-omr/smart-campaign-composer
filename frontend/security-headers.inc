# Browser security headers (security audit R-08). Included in the server block
# and again in every location that sets its own add_header: nginx drops the
# server-level add_header directives in such a location.
#
# CSP: the build has no inline script or style; the one data: URL is a font
# inside the CSS. frame-ancestors 'none' (and X-Frame-Options for older
# browsers) stops other pages from framing the approval dialog.
add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'" always;
add_header X-Frame-Options "DENY" always;
add_header X-Content-Type-Options "nosniff" always;
add_header Referrer-Policy "no-referrer" always;
add_header Permissions-Policy "camera=(), microphone=(), geolocation=(), payment=(), usb=()" always;
add_header Cross-Origin-Opener-Policy "same-origin" always;
