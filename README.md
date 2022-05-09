# OSMViews

World-wide ranking of geographic locations based on OpenStreetMap tile logs.
Updated weekly. Aggregated over the past 52 weeks to smoothen seasonal effects.
For any location on the planet, up to ~150m/z18 resolution.


## Roadmap to 1.0

* Write a Python function `osmviews.load()` to load and refresh
  the local GeoTIFF file. Currently, clients have to download the
  the file manually.

* Write documentation for the Python client.

* Write documentation for the backend pipeline. Document the tricks
  we use to process such a large dataset on a single machine in reasonable
  time.

* Improve the server homepage, display the histogram whose data already
  gets computed.

* Implement the OpenGIS WMTS protocol in the webserver.

* Extend the webserver homepage to display a heatmap. Currently, users
  can already point QGIS or another GIS to our GeoTIFF file, but not many
  people know how to do this.
