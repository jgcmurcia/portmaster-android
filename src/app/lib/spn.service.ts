import { Injectable } from "@angular/core";
import { BehaviorSubject, Observable, from } from "rxjs";
import { filter, map, multicast, refCount } from "rxjs/operators";

import GoBridge from "../plugins/go.bridge";
import { PortapiService } from "./portapi.service";
import { Pin, SPNStatus, UserProfile } from "./spn.types";

@Injectable({ providedIn: 'root' })
export class SPNService {

  /** Emits database-backed SPN status changes when available. */
  status$: Observable<SPNStatus>;

  constructor(private portapi: PortapiService) {
    this.status$ = this.portapi.watch<SPNStatus>('runtime:spn/status')
      .pipe(
        multicast(() => new BehaviorSubject<any | null>(null)),
        refCount(),
        filter(val => val !== null),
      )
  }

  watchPins(): Observable<Pin[]> {
    return this.portapi.watchAll<Pin>("map:main/")
  }

  /**
   * Critical account operations use the direct Go bridge. This bypasses the
   * compatibility HTTP/WebView layer and talks to Safing's SPN access client.
   */
  login({ username, password }: { username: string, password: string }): Observable<string> {
    return from(GoBridge.SPNLogin(username, password));
  }

  logout(_purge = false): Observable<void> {
    return from(GoBridge.SPNLogout());
  }

  userProfile(refresh = false): Observable<UserProfile> {
    const request = refresh
      ? GoBridge.RefreshSPNUserProfile()
      : GoBridge.GetSPNUserProfile();

    return from(request).pipe(
      map(raw => JSON.parse(raw) as UserProfile)
    );
  }

  /**
   * Keep the database subscription for push updates, but callers can use
   * userProfile() as a direct fallback if the database bridge is still warming.
   */
  watchProfile(): Observable<UserProfile | null> {
    let hasSent = false;
    return this.portapi.watch<UserProfile>('core:spn/account/user', {}, { forwardDone: true })
      .pipe(
        filter(result => {
          if ('type' in result && result.type === 'done') {
            return !hasSent;
          }
          return true;
        }),
        map(result => {
          if ('type' in result) {
            return null;
          }
          hasSent = true;
          return result;
        })
      );
  }

  getStatusDirect(): Observable<SPNStatus> {
    return from(GoBridge.GetSPNStatus()).pipe(
      map(raw => JSON.parse(raw) as SPNStatus)
    );
  }
}
