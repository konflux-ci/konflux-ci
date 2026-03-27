// Adapted from code by Matt Walters https://www.mattwalters.net/posts/2018-03-28-hugo-and-lunr/

(function ($) {
  'use strict';

  $(document).ready(function () {
    const $searchInput = $('.td-search input');

    //
    // Register handler
    //

    $searchInput.on('change', (event) => {
      render($(event.target));

      // Hide keyboard on mobile browser
      $searchInput.blur();
    });

    // Prevent reloading page by enter key on sidebar search.
    $searchInput.closest('form').on('submit', () => {
      return false;
    });

    //
    // Lunr
    //

    let idx = null; // Lunr index
    let indexLoadFailed = false;
    const resultDetails = new Map(); // Will hold the data for the search results (titles, summaries, and body)

    // Set up for an Ajax call to request the JSON data file that is created by Hugo's build process
    const indexSrc = $searchInput.data('offline-search-index-json-src');
    $.ajax(indexSrc)
      .then((data) => {
        idx = lunr(function () {
          this.ref('ref');
          this.metadataWhitelist = ['position'];

          // If you added more searchable fields to the search index, list them here.
          this.field('title', { boost: 5 });
          this.field('categories', { boost: 3 });
          this.field('tags', { boost: 3 });
          this.field('description', { boost: 2 });
          this.field('body');

          data.forEach((doc) => {
            this.add(doc);

            resultDetails.set(doc.ref, {
              title: doc.title,
              excerpt: doc.excerpt,
              body: doc.body || '',
            });
          });
        });

        $searchInput.trigger('change');
      })
      .fail((_jqxhr, _textStatus, errorThrown) => {
        indexLoadFailed = true;
        console.error(`[offline-search] Failed to load search index from "${indexSrc}": ${errorThrown}`);
      });

    //
    // Extract a ~250-char snippet from `body` centred on the first match position.
    // Falls back to a plain string search if Lunr position metadata is unavailable.
    //
    function buildSnippet(body, matchData, searchQuery) {
      if (!body) return '';

      // Try to find the match position from Lunr metadata
      let pos = -1;
      const metadata = matchData.metadata;
      for (const term in metadata) {
        if (metadata[term].body && metadata[term].body.position) {
          pos = metadata[term].body.position[0][0];
          break;
        }
      }

      // Fallback: plain text search
      if (pos === -1) {
        const tokens = lunr.tokenizer(searchQuery.toLowerCase());
        for (const token of tokens) {
          const idx = body.toLowerCase().indexOf(token.toString());
          if (idx !== -1) { pos = idx; break; }
        }
      }

      // If the match is only in the title/description/tags (not body), return null
      // so the caller can fall back to doc.excerpt instead of showing an unrelated body slice.
      if (pos === -1) return null;

      // Respect offlineSearchSummaryLength from config.yaml; split evenly around the match
      const summaryLength = parseInt($searchInput.data('offline-search-summary-length'), 10) || 70;
      const snippetRadius = Math.floor(summaryLength / 2);
      const start = Math.max(0, pos - snippetRadius);
      const end   = Math.min(body.length, pos + snippetRadius);
      let snippet = body.slice(start, end).replace(/\s+/g, ' ').trim();

      if (start > 0) snippet = '\u2026' + snippet;
      if (end < body.length) snippet = snippet + '\u2026';

      return snippet;
    }

    //
    // Wrap every occurrence of `term` in the snippet with a <mark> tag.
    // The snippet is HTML-escaped first so that any `<`/`>` in page content
    // (e.g. kubectl placeholders, Go generics) cannot be interpreted as HTML.
    //
    function highlightTerms(snippet, searchQuery) {
      // Escape via jQuery's text → html round-trip, which is the safest
      // cross-browser way to HTML-encode an arbitrary string.
      const escaped = $('<div>').text(snippet).html();
      const tokens = lunr.tokenizer(searchQuery.toLowerCase())
        .map((t) => t.toString().replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
      if (!tokens.length) return escaped;
      const pattern = new RegExp('(' + tokens.join('|') + ')', 'gi');
      return escaped.replace(pattern, '<mark>$1</mark>');
    }

    const render = ($targetSearchInput) => {
      //
      // Dispose existing popover
      //

      {
        let popover = bootstrap.Popover.getInstance($targetSearchInput[0]);
        if (popover !== null) {
          popover.dispose();
        }
      }

      //
      // Search
      //

      if (indexLoadFailed) {
        const $errHtml = $('<div>').append(
          $('<p>').text('Search index failed to load. Please try reloading the page.')
        );
        const popover = new bootstrap.Popover($targetSearchInput, {
          content: $errHtml[0],
          html: true,
          customClass: 'td-offline-search-results',
          placement: 'bottom',
        });
        popover.show();
        return;
      }

      if (idx === null) {
        return;
      }

      const searchQuery = $targetSearchInput.val();
      if (searchQuery === '') {
        return;
      }

      const results = idx
        .query((q) => {
          const tokens = lunr.tokenizer(searchQuery.toLowerCase());
          tokens.forEach((token) => {
            const queryString = token.toString();
            q.term(queryString, {
              boost: 100,
            });
            q.term(queryString, {
              wildcard:
                lunr.Query.wildcard.LEADING | lunr.Query.wildcard.TRAILING,
              boost: 10,
            });
            q.term(queryString, {
              editDistance: 2,
            });
          });
        })
        .slice(0, $targetSearchInput.data('offline-search-max-results'));

      //
      // Make result html
      //

      const $html = $('<div>');

      $html.append(
        $('<div>')
          .css({
            display: 'flex',
            justifyContent: 'space-between',
            marginBottom: '1em',
          })
          .append(
            $('<span>').text('Search results').css({ fontWeight: 'bold' })
          )
          .append(
            $('<span>').addClass('td-offline-search-results__close-button')
          )
      );

      const $searchResultBody = $('<div>').css({
        maxHeight: `calc(100vh - ${
          $targetSearchInput.offset().top - $(window).scrollTop() + 180
        }px)`,
        overflowY: 'auto',
      });
      $html.append($searchResultBody);

      if (results.length === 0) {
        $searchResultBody.append(
          $('<p>').text(`No results found for query "${searchQuery}"`)
        );
      } else {
        results.forEach((r) => {
          const doc = resultDetails.get(r.ref);
          const href =
            $searchInput.data('offline-search-base-href') +
            r.ref.replace(/^\//, '');

          const $entry = $('<div>').addClass('mt-4');

          $entry.append(
            $('<small>').addClass('d-block text-body-secondary').text(r.ref)
          );

          $entry.append(
            $('<a>')
              .addClass('d-block')
              .css({ fontSize: '1.2rem' })
              .attr('href', href)
              .text(doc.title)
          );

          // Build a contextual snippet centred on the match and highlight terms
          const snippet = buildSnippet(doc.body, r.matchData, searchQuery);
          if (snippet) {
            $entry.append(
              $('<p>')
                .addClass('td-search-result__body')
                .css({ fontSize: '0.9rem', color: 'var(--bs-secondary-color)' })
                .html(highlightTerms(snippet, searchQuery))
            );
          } else {
            $entry.append($('<p>').text(doc.excerpt));
          }

          $searchResultBody.append($entry);
        });
      }

      $targetSearchInput.one('shown.bs.popover', () => {
        $('.td-offline-search-results__close-button').on('click', () => {
          $targetSearchInput.val('');
          $targetSearchInput.trigger('change');
        });
      });

      const popover = new bootstrap.Popover($targetSearchInput, {
        content: $html[0],
        html: true,
        customClass: 'td-offline-search-results',
        placement: 'bottom',
      });
      popover.show();
    };
  });
})(jQuery);
