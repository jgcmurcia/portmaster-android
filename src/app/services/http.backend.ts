/**
 * @license
 * Copyright Google LLC All Rights Reserved.
 *
 * Use of this source code is governed by an MIT-style license that can be
 * found in the LICENSE file at https://angular.io/license
 */

import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import { HttpBackend, HttpEvent, HttpRequest, HttpResponse } from '@angular/common/http';
import GoBridge from '../plugins/go.bridge';

@Injectable()
export class HttpGoBackend implements HttpBackend {

  /**
   * Processes a request through the in-process Go bridge. Request and response
   * payloads are intentionally never logged: configuration and account-related
   * endpoints can contain sensitive data.
   */
  handle(req: HttpRequest<any>): Observable<HttpEvent<any>> {
    return new Observable<HttpEvent<any>>((subscriber) => {
      const requestJson: {
        url: string;
        method: string;
        body: any;
        headers: {[key: string]: string[] | null};
      } = {
        url: req.urlWithParams,
        method: req.method,
        body: req.serializeBody(),
        headers: {},
      };

      req.headers.keys().forEach((key: string) => {
        requestJson.headers[key] = req.headers.getAll(key);
      });

      let cancelled = false;
      GoBridge.PerformRequest({ requestJson: JSON.stringify(requestJson) })
        .then((body: any) => {
          if (!cancelled) {
            subscriber.next(new HttpResponse<any>({ body: body.data }));
          }
        })
        .catch((err: string) => {
          if (!cancelled) {
            subscriber.error(err);
          }
        })
        .finally(() => {
          if (!cancelled) {
            subscriber.complete();
          }
        });

      return () => {
        // The native call cannot currently be canceled, but suppress delivery
        // to a destroyed Angular subscriber to avoid stale UI updates.
        cancelled = true;
      };
    });
  }
}
