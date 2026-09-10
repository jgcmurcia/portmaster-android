import { Injectable, TrackByFunction } from '@angular/core';
import { Params, Router } from '@angular/router';
import { BehaviorSubject, defer, Observable, throwError } from 'rxjs';
import { map, multicast, refCount, toArray } from 'rxjs/operators';

import { PortapiService } from '../lib/portapi.service';
import { RetryableOpts } from '../lib/portapi.types';
import JavaBridge from '../plugins/java.bridge';
import { Action, ActionHandler, NetqueryAction, Notification, NotificationState, NotificationType, OpenPageAction, OpenProfileAction, OpenSettingAction, OpenURLAction, PageIDs, WebhookAction } from './notifications.types';

@Injectable({
  providedIn: 'root'
})
export class NotificationsService {
  static trackBy: TrackByFunction<Notification<any>> = function (_: number, n: Notification<any>) {
    return n.EventID;
  };

  trackBy = NotificationsService.trackBy;
  readonly notificationPrefix = "notifications:all/";
  readonly new$: Observable<Notification<any>[]>;

  private actionHandler: {
    [key in Action['Type']]: (a: any) => Promise<any>;
  } = {
    '': async () => { },
    'open-url': async (a: OpenURLAction) => {
      if (typeof a.Payload !== 'string') {
        return Promise.reject('Invalid notification URL');
      }
      // Notification payloads originate in the backend/update data. Never hand
      // them directly to window.open; the native bridge enforces HTTPS-only.
      return JavaBridge.openUrlInBrowser({url: a.Payload});
    },
    'open-profile': (_a: OpenProfileAction) => {
      return Promise.reject("not yet supported");
    },
    'open-setting': (a: OpenSettingAction) => {
      return this.router.navigate(['/settings'], {
        queryParams: {
          setting: a.Payload.Key
        }
      });
    },
    'open-page': (a: OpenPageAction) => {
      let pageID: keyof typeof PageIDs | null = null;
      let queryParams: Params | null = null;

      if (typeof a.Payload === 'string') {
        pageID = a.Payload;
        queryParams = {};
      } else {
        pageID = a.Payload.id;
        queryParams = a.Payload.query;
      }

      const route = PageIDs[pageID];
      if (!!route) {
        return this.router.navigate([route], {queryParams});
      }
      return Promise.reject('not yet supported');
    },
    'ui': (a: ActionHandler<any>) => {
      return a.Run(a);
    },
    'netquery': (a: NetqueryAction) => {
      return this.router.navigate(['/monitor'], {
        queryParams: {q: a.Payload}
      });
    },
    'call-webhook': (_a: WebhookAction) => {
      return Promise.reject('Webhooks not implemented');
    }
  };

  constructor(
    private portapi: PortapiService,
    private router: Router,
  ) {
    this.new$ = this.watchAll().pipe(
      map(msgs => msgs.filter(msg => msg.State === NotificationState.Active || !msg.State)),
      multicast(() => new BehaviorSubject<Notification<any>[]>([])),
      refCount(),
    );
  }

  watchAll<T = any>(query: string = '', opts?: RetryableOpts): Observable<Notification<T>[]> {
    return this.portapi.watchAll<Notification<T>>(this.notificationPrefix + query, opts);
  }

  query(query: string): Observable<Notification<any>[]> {
    return this.portapi.query<Notification<any>>(this.notificationPrefix + query)
      .pipe(
        map(value => value.data),
        toArray()
      );
  }

  get<T>(id: string): Observable<Notification<T>> {
    return this.portapi.get(this.notificationPrefix + id);
  }

  execute(n: Notification<any>, action: Action): Observable<void>;
  execute(notificationId: string, action: Action): Observable<void>;
  execute(notifOrId: Notification<any> | string, action: Action): Observable<void> {
    const payload: Partial<Notification<any>> = {};
    payload.EventID = typeof notifOrId === 'string' ? notifOrId : notifOrId.EventID;

    return defer(async () => {
      await this.performAction(action);

      if (!!action.ID) {
        payload.SelectedActionID = action.ID;
        const key = this.notificationPrefix + payload.EventID;
        await this.portapi.update(key, payload).toPromise();
      }
    });
  }

  async performAction(action: Action) {
    if (!action.Type) {
      return;
    }

    const handler = this.actionHandler[action.Type] as (a: Action) => Promise<any>;
    if (!handler) {
      throw new Error(`Cannot handle notification action type ${action.Type}`);
    }
    await handler(action);
  }

  resolvePending(n: Notification<any>, time?: number): Observable<void>;
  resolvePending(n: string, time?: number): Observable<void>;
  resolvePending(notifOrID: Notification<any> | string, time: number = Math.round(Date.now() / 1000)): Observable<void> {
    const payload: Partial<Notification<any>> = {};
    if (typeof notifOrID === 'string') {
      payload.EventID = notifOrID;
    } else {
      payload.EventID = notifOrID.EventID;
      if (notifOrID.State === NotificationState.Executed) {
        return throwError(`Notification ${notifOrID.EventID} already executed`);
      }
    }

    payload.State = NotificationState.Responded;
    const key = this.notificationPrefix + payload.EventID;
    return this.portapi.update(key, payload);
  }

  delete(n: Notification<any>): Observable<void>;
  delete(id: string): Observable<void>;
  delete(notifOrId: Notification<any> | string): Observable<void> {
    return this.portapi.delete(typeof notifOrId === 'string' ? notifOrId : notifOrId.EventID);
  }

  create(n: Partial<Notification<any>>): Observable<void>;
  create(id: string, message: string, type: NotificationType, args?: Partial<Notification<any>>): Observable<void>;
  create(notifOrId: Partial<Notification<any>> | string, message?: string, type?: NotificationType, args?: Partial<Notification<any>>): Observable<void> {
    if (typeof notifOrId === 'string') {
      notifOrId = {
        ...args,
        EventID: notifOrId,
        State: NotificationState.Active,
        Message: message,
        Type: type,
      } as Notification<any>;
    }

    if (!notifOrId.EventID) {
      return throwError('Notification ID is required');
    }
    if (!notifOrId.Message) {
      return throwError('Notification message is required');
    }
    if (typeof notifOrId.Type !== 'number') {
      return throwError('Notification type is required');
    }

    return this.portapi.create(this.notificationPrefix + notifOrId.EventID, notifOrId);
  }
}
