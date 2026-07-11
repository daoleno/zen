export type WindowScene = {
  id: string;
  title: string;
  uri: string;
  attribution: string;
  license: string;
  sourceUrl: string;
};

export type WindowScenePage = { scenes: WindowScene[]; cursor: string | null };

const MIXKIT_SCENES = [
  ["2213", "Waterfall in forest"], ["4075", "Countryside meadow"],
  ["51502", "Rocky coast and waves"], ["51445", "Beach skyline at sunset"],
  ["4881", "Field of sunflowers"], ["50847", "Tranquil forest"],
  ["5016", "Waves coming to the beach"], ["26108", "Clouds crossing blue sky"],
  ["1564", "White sand beach"], ["22728", "Rain in a cloud forest"],
  ["1187", "White flowers in the breeze"], ["51492", "Sunrise on a Pacific beach"],
  ["1164", "Waves on open water"],
  ["40657", "Sunlit grass meadow"], ["18312", "Rain falling on a lake"],
  ["22729", "Rainy forest afternoon"], ["44496", "Peaceful beach sunset"],
  ["5030", "Savanna lake"], ["572", "Waterfall landscape"],
  ["5040", "Great green forest"], ["1954", "Sea waves in a small bay"],
  ["529", "Forest stream in sunlight"], ["44499", "Palm beach sunset"],
  ["4078", "Ocean waves on the coast"], ["570", "Water among rocks"],
  ["3128", "Mountain sunset"], ["40047", "Desert moon at night"],
  ["5039", "Dense green jungle"],
  ["51105", "Fast clouds in a clear sky"], ["44498", "Calm beach at sunset"],
  ["41573", "Path through a forest"], ["18310", "Rain on tree leaves"],
  ["51447", "River through a forest"], ["95", "Branches moving in the wind"],
  ["1944", "Beautiful sunrise landscape"], ["51590", "River water over rocks"],
  ["1188", "Tree branches in the breeze"],
  ["51102", "Clouds moving across blue sky"], ["25542", "Rocky river waterfall"],
  ["4038", "Blue and green northern lights"], ["2717", "Rain in the wild"],
  ["50867", "Forest in morning mist"], ["51505", "Foaming waves on shore"],
  ["51432", "Rocks on the seashore"], ["50160", "Sun and clouds timelapse"],
  ["51662", "Calm creek with dawn mist"],
] as const;

const PAGE_SIZE = 12;

export async function fetchWindowScenePage(
  cursor: string | null,
  _signal?: AbortSignal,
): Promise<WindowScenePage> {
  const start = Number.parseInt(cursor ?? "0", 10) || 0;
  const scenes = Array.from({ length: Math.min(PAGE_SIZE, MIXKIT_SCENES.length) }, (_, offset) => {
    const [id, title] = MIXKIT_SCENES[(start + offset) % MIXKIT_SCENES.length]!;
    return {
      id,
      title,
      uri: `https://assets.mixkit.co/videos/${id}/${id}-720.mp4`,
      attribution: `${title} · Mixkit`,
      license: "Mixkit Free License",
      sourceUrl: `https://mixkit.co/free-stock-video/${id}/`,
    };
  });
  return {
    scenes,
    cursor: String((start + PAGE_SIZE) % MIXKIT_SCENES.length),
  };
}
