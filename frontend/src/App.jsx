// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

export default function App() {
  return (
    <main>
      <h1>
        <span className="osm">OSM</span>Views <small>beta</small>
      </h1>
      <p>
        The interactive map lives here soon. For now this is the <code>/beta/</code>{' '}
        app shell — Phase 1 of{' '}
        <a href="https://github.com/brawer/osmviews/issues/100">issue #100</a>,
        validating the Node&nbsp;+&nbsp;Go buildpack pipeline end to end.
      </p>
      <p>
        The stable landing page and all <code>/download/</code> files are
        unaffected.
      </p>
    </main>
  );
}
