import { CommonModule, LocationStrategy } from '@angular/common';
import { Component, OnDestroy, OnInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { IonicModule } from '@ionic/angular';
import { Subscription } from 'rxjs';

import { SPNService } from '../../lib/spn.service';
import GoBridge from '../../plugins/go.bridge';

interface CountryOption {
  code: string;
  name: string;
}

@Component({
  selector: 'app-spn-routing',
  templateUrl: './spn-routing.component.html',
  styleUrls: ['./spn-routing.component.scss'],
  standalone: true,
  imports: [CommonModule, IonicModule, FormsModule]
})
export class SpnRoutingComponent implements OnInit, OnDestroy {
  selectedCountry = '';
  countries: CountryOption[] = [];
  saving = false;
  error = '';

  private pinSubscription = Subscription.EMPTY;

  private readonly names: {[key: string]: string} = {
    AT: 'Austria', BE: 'Belgium', CA: 'Canada', CH: 'Switzerland',
    CZ: 'Czechia', DE: 'Germany', DK: 'Denmark', EE: 'Estonia',
    ES: 'Spain', FI: 'Finland', FR: 'France', GB: 'United Kingdom',
    IE: 'Ireland', IL: 'Israel', IT: 'Italy', JP: 'Japan',
    LT: 'Lithuania', LU: 'Luxembourg', LV: 'Latvia', NL: 'Netherlands',
    NO: 'Norway', PL: 'Poland', PT: 'Portugal', RO: 'Romania',
    SE: 'Sweden', SG: 'Singapore', US: 'United States', AU: 'Australia'
  };

  // Countries present in the Android SPN generation this fork was built for.
  // The list is expanded dynamically from the live SPN map below.
  private readonly fallbackCodes = ['CA', 'FI', 'FR', 'DE', 'IL', 'PL', 'GB', 'US'];

  constructor(
    private spnService: SPNService,
    private location: LocationStrategy
  ) {}

  async ngOnInit() {
    this.setCountries(this.fallbackCodes);

    try {
      this.selectedCountry = await GoBridge.GetSPNExitCountry();
    } catch (err) {
      this.error = this.errorText(err);
    }

    this.pinSubscription = this.spnService.watchPins().subscribe({
      next: pins => {
        const codes = new Set<string>(this.fallbackCodes);
        for (const pin of pins || []) {
          const v4 = pin?.EntityV4?.Country;
          const v6 = pin?.EntityV6?.Country;
          if (v4 && /^[A-Za-z]{2}$/.test(v4)) {
            codes.add(v4.toUpperCase());
          }
          if (v6 && /^[A-Za-z]{2}$/.test(v6)) {
            codes.add(v6.toUpperCase());
          }
        }
        this.setCountries(Array.from(codes));
      },
      error: () => {
        // The fallback remains usable while the SPN map is starting.
      }
    });
  }

  ngOnDestroy() {
    this.pinSubscription.unsubscribe();
  }

  private setCountries(codes: string[]) {
    this.countries = codes
      .filter(code => /^[A-Z]{2}$/.test(code))
      .map(code => ({code, name: this.names[code] || code}))
      .sort((a, b) => a.name.localeCompare(b.name));
  }

  async save() {
    this.saving = true;
    this.error = '';
    try {
      await GoBridge.SetSPNExitCountry(this.selectedCountry);
      // Close existing app connections so the new exit policy is applied to
      // newly-created SPN routes immediately.
      await GoBridge.RestartTunnel();
      this.location.back();
    } catch (err) {
      this.error = this.errorText(err);
    } finally {
      this.saving = false;
    }
  }

  close() {
    this.location.back();
  }

  private errorText(err: any): string {
    if (!err) {
      return 'Unknown error';
    }
    return err?.message || err?.errorMessage || String(err);
  }
}
