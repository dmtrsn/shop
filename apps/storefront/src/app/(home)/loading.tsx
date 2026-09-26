import { HomeHero } from "@/components/storefront/home-page";

export default function HomeLoading() {
  return (
    <main aria-busy="true" id="main-content">
      <HomeHero />
    </main>
  );
}
