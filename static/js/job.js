'use strict';

// By default, we only show the last section in our output
for (let x of document.querySelectorAll(".section:not(:last-child) .section-toggle")) { x.checked = false; }

// If user checks the 'auto-scroll' checkbox, we'll attempt to scroll to the bottom of the page as soon as new
// information arrives
let scroller = null;
document.getElementById("auto-scroll-toggle").addEventListener("change", function() {
    if (this.checked) {
        let lastMaxY = 0;
        scroller = window.setInterval(function () {
            let body = document.body,
                html = document.documentElement;

            let height = Math.max(body.scrollHeight, body.offsetHeight,
                html.clientHeight, html.scrollHeight, html.offsetHeight);
            if (height > lastMaxY) {
                window.scrollTo(window.scrollX, height);
                lastMaxY = height;
            }
        }, 100);
    } else if (scroller !== null) {
        window.clearInterval(scroller);
    }
});