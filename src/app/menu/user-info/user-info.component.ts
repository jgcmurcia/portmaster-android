import { Component, OnInit } from '@angular/core';
import { CommonModule, LocationStrategy } from '@angular/common';
import { UserProfile } from 'src/app/lib/spn.types';
import { SPNService } from 'src/app/lib/spn.service';
import { IonicModule } from '@ionic/angular';

@Component({
  selector: 'app-user-info',
  templateUrl: './user-info.component.html',
  styleUrls: ['./user-info.component.scss'],
  standalone: true,
  imports: [CommonModule, IonicModule]
})
export class UserInfoComponent implements OnInit {

  User: UserProfile;
  Error: string = "";
  Loading: boolean = true;

  constructor(
    private spnService: SPNService,
    private location: LocationStrategy) { }

  ngOnInit(): void {
    this.spnService.userProfile(false).subscribe({
      next: user => {
        this.User = user;
        this.Loading = false;
      },
      error: err => {
        this.Error = err?.message || String(err);
        this.Loading = false;
      }
    });

    this.spnService.watchProfile().subscribe((user) => {
      if (user?.state !== '') {
        this.User = user || null;
      } else if (user === null) {
        // Keep the direct profile until an explicit logout arrives.
      } else {
        this.User = null;
      }
    });
  }

  refresh(): void {
    this.Error = "";
    this.Loading = true;
    this.spnService.userProfile(true).subscribe({
      next: user => {
        this.User = user;
        this.Loading = false;
      },
      error: err => {
        this.Error = err?.message || String(err);
        this.Loading = false;
      }
    });
  }

  public logout(): void {
    this.spnService.logout(true).subscribe(() => {
      this.User = null;
      this.location.back();
    });
  }
}
